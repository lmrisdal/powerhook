//go:build linux

package service

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"powerhook/internal/config"
)

// The firewalld rules hang off a "powerhook" service definition and the ufw
// rules carry a "powerhook" comment, so they can be found and replaced/removed
// without touching anything else.
const fwName = "powerhook"

// openFirewall (re)creates rules allowing the API port from allowed_ips only,
// or from anywhere when allowed_ips is empty.
func OpenFirewall(cfg *config.Config) error {
	if err := RequireAdmin("firewall"); err != nil {
		return err
	}
	port := cfg.Port()
	switch {
	case firewalldActive():
		_ = removeFirewalld()
		steps := [][]string{
			{"--permanent", "--new-service=" + fwName},
			{"--permanent", "--service=" + fwName, "--set-description=Powerhook remote power API"},
			{"--permanent", "--service=" + fwName, "--add-port=" + port + "/tcp"},
		}
		if cfg.AllowsAnyIP() {
			// Open to everyone: add the service to the default zone.
			steps = append(steps, []string{"--permanent", "--add-service=" + fwName})
		} else {
			for _, p := range cfg.Allowed {
				family := "ipv4"
				if p.Addr().Is6() {
					family = "ipv6"
				}
				rule := fmt.Sprintf(`rule family="%s" source address="%s" service name="%s" accept`, family, p, fwName)
				steps = append(steps, []string{"--permanent", "--add-rich-rule=" + rule})
			}
		}
		steps = append(steps, []string{"--reload"})
		for _, s := range steps {
			if err := runQuiet("firewall-cmd", s...); err != nil {
				return err
			}
		}
		log.Printf("firewalld: allowed TCP %s from %s", port, cfg.AllowedList())
	case ufwActive():
		_ = removeUfw()
		sources := make([]string, 0, len(cfg.Allowed))
		for _, p := range cfg.Allowed {
			sources = append(sources, p.String())
		}
		if cfg.AllowsAnyIP() {
			sources = []string{"any"} // one rule for both IPv4 and IPv6
		}
		for _, src := range sources {
			if err := runQuiet("ufw", "allow", "from", src, "to", "any", "port", port, "proto", "tcp", "comment", fwName); err != nil {
				return err
			}
		}
		log.Printf("ufw: allowed TCP %s from %s", port, cfg.AllowedList())
	default:
		log.Printf("no active firewalld or ufw found; if you use another firewall, allow TCP %s from %s",
			port, cfg.AllowedList())
	}
	return nil
}

// FirewallBackend names the firewall OpenFirewall would configure, or "" if
// neither firewalld nor ufw is active.
func FirewallBackend() string {
	switch {
	case firewalldActive():
		return "firewalld"
	case ufwActive():
		return "ufw"
	}
	return ""
}

// FirewallCurrent reports whether powerhook's rules on the active firewall
// already allow exactly cfg's port from cfg's allowed_ips, read back from the
// firewall itself.
func FirewallCurrent(cfg *config.Config) bool {
	port := cfg.Port()
	switch FirewallBackend() {
	case "firewalld":
		if exec.Command("firewall-cmd", "--permanent", "--info-service="+fwName).Run() != nil {
			return false
		}
		ports, err := exec.Command("firewall-cmd", "--permanent", "--service="+fwName, "--get-ports").Output()
		if err != nil || strings.TrimSpace(string(ports)) != port+"/tcp" {
			return false
		}
		rules, err := exec.Command("firewall-cmd", "--permanent", "--list-rich-rules").Output()
		if err != nil {
			return false
		}
		sources := parseFirewalldRichRules(string(rules))
		inZone := exec.Command("firewall-cmd", "--permanent", "--query-service="+fwName).Run() == nil
		if cfg.AllowsAnyIP() {
			return inZone && len(sources) == 0
		}
		return !inZone && samePrefixes(cfg.Allowed, sources)
	case "ufw":
		out, err := exec.Command("ufw", "status").Output()
		if err != nil {
			return false
		}
		sources, portsOK := parseUfwStatus(string(out), port)
		return portsOK && len(sources) > 0 && rulesMatch(cfg, sources)
	}
	return false
}

// firewallInstalled reports which firewall has powerhook rules, if any.
func FirewallInstalled() string {
	if firewalldActive() && exec.Command("firewall-cmd", "--permanent", "--info-service="+fwName).Run() == nil {
		return "firewalld"
	}
	if ufwActive() && len(ufwRuleNumbers()) > 0 {
		return "ufw"
	}
	return ""
}

func CloseFirewall() error {
	switch FirewallInstalled() {
	case "firewalld":
		if err := removeFirewalld(); err != nil {
			return err
		}
		return runQuiet("firewall-cmd", "--reload")
	case "ufw":
		return removeUfw()
	}
	return nil
}

func firewalldActive() bool {
	return exec.Command("firewall-cmd", "--state").Run() == nil
}

func removeFirewalld() error {
	out, err := exec.Command("firewall-cmd", "--permanent", "--list-rich-rules").Output()
	if err != nil {
		return err
	}
	for _, rule := range strings.Split(string(out), "\n") {
		if strings.Contains(rule, `service name="`+fwName+`"`) {
			_ = runQuiet("firewall-cmd", "--permanent", "--remove-rich-rule="+strings.TrimSpace(rule))
		}
	}
	if exec.Command("firewall-cmd", "--permanent", "--info-service="+fwName).Run() == nil {
		// Added to the default zone when allowed_ips is empty.
		if exec.Command("firewall-cmd", "--permanent", "--query-service="+fwName).Run() == nil {
			_ = runQuiet("firewall-cmd", "--permanent", "--remove-service="+fwName)
		}
		return runQuiet("firewall-cmd", "--permanent", "--delete-service="+fwName)
	}
	return nil
}

func ufwActive() bool {
	out, err := exec.Command("ufw", "status").Output()
	return err == nil && strings.Contains(string(out), "Status: active")
}

var ufwNumbered = regexp.MustCompile(`^\[\s*(\d+)\]`)

// ufwRuleNumbers returns the numbers of powerhook's ufw rules, highest first
// (so deleting in order doesn't shift the remaining numbers).
func ufwRuleNumbers() []int {
	out, err := exec.Command("ufw", "status", "numbered").Output()
	if err != nil {
		return nil
	}
	var nums []int
	for _, line := range strings.Split(string(out), "\n") {
		if !strings.HasSuffix(strings.TrimSpace(line), "# "+fwName) {
			continue
		}
		if m := ufwNumbered.FindStringSubmatch(line); m != nil {
			n, _ := strconv.Atoi(m[1])
			nums = append(nums, n)
		}
	}
	sort.Sort(sort.Reverse(sort.IntSlice(nums)))
	return nums
}

func removeUfw() error {
	for _, n := range ufwRuleNumbers() {
		if err := runQuiet("ufw", "--force", "delete", strconv.Itoa(n)); err != nil {
			return err
		}
	}
	return nil
}

func runQuiet(name string, args ...string) error {
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		fmt.Fprint(os.Stderr, string(out))
		return fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return nil
}
