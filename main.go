// Command powerhook exposes suspend/poweroff/restart over a small HTTP API,
// restricted by IP allowlist and a bearer token.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"powerhook/internal/config"
	"powerhook/internal/power"
	"powerhook/internal/server"
	"powerhook/internal/service"
	"powerhook/internal/update"
)

var version = "0.1.0"

const usage = `Powerhook - suspend, power off or restart this PC over HTTP

Usage:
  powerhook [--config PATH] [command]

Commands:
  (none)           Run the API server. This is what the service runs.
  status           Validate the config and print a summary (including commands).
  reload [-y | -n]  Apply the config: validate it, check the firewall rule
                   for allowed_ips (firewalld, ufw or Windows Firewall) and
                   ask before changing it, then start or restart the
                   service. -y/--yes answers yes to every question,
                   -n/--no answers no. Needs sudo / Administrator.
  update [--force] Download and install the latest powerhook release, then
                   restart the service. Needs sudo / Administrator.
  uninstall        Interactively remove powerhook (needs sudo / Administrator).
                   Lists what's installed, then lets you pick what to remove,
                   or "everything" (service + firewall + binary + config).
  version          Print version.
`

func printUsage(w *os.File) {
	fmt.Fprint(w, usage)
}

func main() {
	log.SetFlags(0)
	log.SetPrefix("powerhook: ")
	if service.IsService() {
		// No console under the Windows service manager; log to a file instead.
		if f, err := service.OpenLog(); err == nil {
			log.SetOutput(f)
			log.SetFlags(log.LstdFlags)
		}
	}

	fs := flag.NewFlagSet("powerhook", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	configPath := fs.String("config", "", "path to config file")
	fs.Usage = func() { printUsage(os.Stderr) }
	if err := fs.Parse(os.Args[1:]); err != nil {
		os.Exit(2)
	}

	cmd, rest := "", []string(nil)
	answer := "ask" // reload's answer to every question: "ask", "yes" or "no"
	if args := fs.Args(); len(args) > 0 {
		cmd, rest = args[0], args[1:]
	}

	switch cmd {
	case "version":
		fmt.Println("Powerhook", version)
		return
	case "help", "-h", "--help":
		printUsage(os.Stdout)
		return
	case "update":
		force := false
		upFs := flag.NewFlagSet("update", flag.ContinueOnError)
		upFs.BoolVar(&force, "force", false, "reinstall even if already on the latest version")
		if err := upFs.Parse(rest); err != nil {
			os.Exit(2)
		}
		if err := update.Run(version, force); err != nil {
			log.Fatal(err)
		}
		return
	case "uninstall":
		if err := service.Uninstall(); err != nil {
			log.Fatal(err)
		}
		return
	case "reload":
		if err := service.RequireAdmin("reload"); err != nil {
			log.Fatal(err)
		}
		reFs := flag.NewFlagSet("reload", flag.ContinueOnError)
		yes, no := new(bool), new(bool)
		reFs.BoolVar(yes, "yes", false, "answer yes to every question")
		reFs.BoolVar(yes, "y", false, "short for --yes")
		reFs.BoolVar(no, "no", false, "answer no to every question")
		reFs.BoolVar(no, "n", false, "short for --no")
		if err := reFs.Parse(rest); err != nil {
			os.Exit(2)
		}
		switch {
		case *yes && *no:
			log.Fatal("-y/--yes and -n/--no can't be used together")
		case *yes:
			answer = "yes"
		case *no:
			answer = "no"
		}
	case "", "status":
		// known; load config below
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", cmd)
		printUsage(os.Stderr)
		os.Exit(2)
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatal(err)
	}

	switch cmd {
	case "":
		err = run(cfg)
	case "status":
		status(cfg)
	case "reload":
		err = reload(cfg, answer)
	}
	if err != nil {
		log.Fatal(err)
	}
}

func run(cfg *config.Config) error {
	update.CleanupOld()
	var ctl power.Controller = power.New(power.Options{ForceCloseApps: cfg.ForceCloseApps})
	if cfg.DryRun {
		ctl = power.DryRun{}
	}
	srv := server.New(cfg, ctl, version)
	if service.IsService() {
		return service.Run(srv.Run)
	}
	warnIfReadable(cfg.Path)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return srv.Run(ctx)
}

// reload applies the config: it asks whether to update the firewall, then
// starts or restarts the service. Load has already validated the config, so a
// broken one never takes the running service down.
//
// answer is "ask" (prompt on the terminal), or "yes"/"no" for every question
// (from -y/-n). With no terminal to ask on, existing powerhook rules are
// updated and none are added.
func reload(cfg *config.Config, answer string) error {
	printCommandWarnings(cfg)
	if err := reloadFirewall(cfg, answer); err != nil {
		return err
	}
	installed, err := service.Restart()
	if err != nil {
		return fmt.Errorf("restart service: %w", err)
	}
	if !installed {
		fmt.Println("Config is valid, but the Powerhook service isn't installed.")
		return nil
	}
	if cfg.Unprotected() {
		fmt.Fprintf(os.Stderr, "\n%s\n\n", config.UnprotectedWarning)
	}
	fmt.Println("The service is running with the new config.")
	return nil
}

func reloadFirewall(cfg *config.Config, choice string) error {
	backend := service.FirewallBackend()
	if backend == "" {
		fmt.Printf("No active firewalld or ufw found, so no firewall rules were added. If another\n"+
			"firewall blocks incoming traffic, allow TCP %s from %s yourself.\n", cfg.Port(), cfg.AllowedList())
		return nil
	}
	if choice != "no" && service.FirewallCurrent(cfg) {
		return nil // rules already match; nothing to ask or change
	}
	existing := service.FirewallInstalled() != ""
	apply := choice == "yes"
	if choice == "ask" {
		verb := "Add a"
		if existing {
			verb = "Update the"
		}
		q := fmt.Sprintf("%s %s rule allowing TCP %s from %s?", verb, backend, cfg.Port(), cfg.AllowedList())
		var ok bool
		if apply, ok = service.AskYesNo(q, true); !ok {
			apply = existing // nobody to ask: keep existing rules in sync, add none
		}
	}
	switch {
	case apply:
		if err := service.OpenFirewall(cfg); err != nil {
			return fmt.Errorf("update firewall: %w", err)
		}
	case existing:
		fmt.Println("Left the existing firewall rules as they are; they may not match allowed_ips.")
	default:
		fmt.Printf("No firewall rule added. If %s blocks incoming traffic, allow TCP %s yourself\n"+
			"or run \"powerhook reload\" again and answer yes.\n", backend, cfg.Port())
	}
	return nil
}

func status(cfg *config.Config) {
	fmt.Printf("config:       %s\n", cfg.Path)
	fmt.Printf("listen:       %s\n", cfg.ListenAddr())
	fmt.Printf("allowed_ips:  %s\n", cfg.AllowedList())
	if cfg.TokenRequired() {
		fmt.Println("token:        required")
	} else {
		fmt.Println("token:        not required")
	}
	fmt.Printf("action delay: %s\n", cfg.ActionDelay())
	fmt.Printf("dry_run:      %v\n", cfg.DryRun)
	if names := cfg.CommandNames(); len(names) > 0 {
		fmt.Printf("commands:     %s\n", strings.Join(names, ", "))
	}
	printCommandWarnings(cfg)
	if cfg.Unprotected() {
		// stderr, so it still shows when the installers discard stdout.
		fmt.Fprintf(os.Stderr, "\n%s\n\n", config.UnprotectedWarning)
	}
	fmt.Println("OK")
}

func printCommandWarnings(cfg *config.Config) {
	for _, w := range cfg.CommandWarnings() {
		fmt.Fprintf(os.Stderr, "warning: %s\n", w)
	}
}

// warnIfReadable warns when the config (which holds the token) is readable by
// other users. Windows permissions are handled by install.ps1.
func warnIfReadable(path string) {
	if st, err := os.Stat(path); err == nil && st.Mode().Perm()&0o044 != 0 && os.PathSeparator == '/' {
		log.Printf("warning: %s is readable by other users and contains the token (fix: chmod 600 %s)", path, path)
	}
}
