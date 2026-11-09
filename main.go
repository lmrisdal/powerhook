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

const usage = `powerhook - suspend, power off or restart this PC over HTTP

Usage:
  powerhook [--config PATH] [command]

Commands:
  (none)           Run the API server. This is what the service runs.
  status           Validate the config and print a summary.
  firewall         Allow the API port from allowed_ips in firewalld, ufw or
                   Windows Firewall. Re-run after changing listen or
                   allowed_ips. Needs sudo / Administrator.
  token            Print a new random token.
  update [--force] Download and install the latest powerhook release, then
                   restart the service. Needs sudo / Administrator.
  uninstall        Interactively remove powerhook (needs sudo / Administrator).
                   Lists what's installed, then lets you pick what to remove,
                   or "everything" (service + firewall + binary + config).
  version          Print version.

Config is searched in this order when --config is omitted:
  %s
`

func printUsage(w *os.File) {
	fmt.Fprintf(w, usage, strings.Join(config.SearchPaths(), ", "))
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
	if args := fs.Args(); len(args) > 0 {
		cmd, rest = args[0], args[1:]
	}

	switch cmd {
	case "version":
		fmt.Println("powerhook", version)
		return
	case "help", "-h", "--help":
		printUsage(os.Stdout)
		return
	case "token":
		fmt.Println(config.NewToken())
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
	case "", "status", "firewall":
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
	case "firewall":
		err = service.OpenFirewall(cfg)
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

func status(cfg *config.Config) {
	fmt.Printf("config:       %s\n", cfg.Path)
	fmt.Printf("listen:       %s\n", cfg.ListenAddr())
	fmt.Printf("allowed_ips:  %s\n", strings.Join(cfg.AllowedIPs, ", "))
	if cfg.TokenRequired() {
		fmt.Println("token:        required")
	} else {
		fmt.Println("token:        not required (IP allowlist only)")
	}
	fmt.Printf("action delay: %s\n", cfg.ActionDelay())
	fmt.Printf("dry_run:      %v\n", cfg.DryRun)
	fmt.Println("OK")
}

// warnIfReadable warns when the config (which holds the token) is readable by
// other users. Windows permissions are handled by install.ps1.
func warnIfReadable(path string) {
	if st, err := os.Stat(path); err == nil && st.Mode().Perm()&0o044 != 0 && os.PathSeparator == '/' {
		log.Printf("warning: %s is readable by other users and contains the token (fix: chmod 600 %s)", path, path)
	}
}
