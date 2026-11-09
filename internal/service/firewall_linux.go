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

// openFirewall (re)creates rules allowing the API port from allowed_ips only.
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
			{"--permanent", "--service=" + fwName, "--set-description=powerhook remote power API"},
			{"--permanent", "--service=" + fwName, "--add-port=" + port + "/tcp"},
		}
		for _, p := range cfg.Allowed {
			family := "ipv4"
			if p.Addr().Is6() {
				family = "ipv6"
			}
			rule := fmt.Sprintf(`rule family="%s" source address="%s" service name="%s" accept`, family, p, fwName)
			steps = append(steps, []string{"--permanent", "--add-rich-rule=" + rule})
		}
		steps = append(steps, []string{"--reload"})
		for _, s := range steps {
			if err := runQuiet("firewall-cmd", s...); err != nil {
				return err
			}
		}
		log.Printf("firewalld: allowed TCP %s from %s", port, strings.Join(cfg.AllowedIPs, ", "))
	case ufwActive():
		_ = removeUfw()
		for _, p := range cfg.Allowed {
			if err := runQuiet("ufw", "allow", "from", p.String(), "to", "any", "port", port, "proto", "tcp", "comment", fwName); err != nil {
				return err
			}
		}
		log.Printf("ufw: allowed TCP %s from %s", port, strings.Join(cfg.AllowedIPs, ", "))
	default:
		log.Printf("no active firewalld or ufw found; if you use another firewall, allow TCP %s from %s",
			port, strings.Join(cfg.AllowedIPs, ", "))
	}
	return nil
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
