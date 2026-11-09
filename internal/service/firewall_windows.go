//go:build windows

package service

import (
	"fmt"
	"log"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"powerhook/internal/config"
)

const fwName = "powerhook"

// openFirewall (re)creates an inbound Windows Firewall rule for the API port,
// limited to allowed_ips.
func OpenFirewall(cfg *config.Config) error {
	if err := RequireAdmin("firewall"); err != nil {
		return err
	}
	remote := make([]string, 0, len(cfg.Allowed))
	for _, p := range cfg.Allowed {
		if p.Bits() == 0 {
			remote = []string{"any"}
			break
		}
		remote = append(remote, p.String())
	}
	program, err := os.Executable()
	if err != nil {
		return err
	}
	if p, err := filepath.EvalSymlinks(program); err == nil {
		program = p
	}
	_ = CloseFirewall()
	// profile=any: home networks are often classified as "Public"; the remoteip
	// restriction is what limits access.
	out, err := exec.Command("netsh", "advfirewall", "firewall", "add", "rule",
		"name="+fwName, "dir=in", "action=allow", "protocol=TCP",
		"localport="+cfg.Port(), "remoteip="+strings.Join(remote, ","),
		"profile=any", "program="+program, "enable=yes").CombinedOutput()
	if err != nil {
		return fmt.Errorf("add firewall rule: %w: %s", err, strings.TrimSpace(string(out)))
	}
	log.Printf("Windows Firewall: allowed TCP %s from %s", cfg.Port(), strings.Join(remote, ", "))
	return nil
}

// FirewallBackend names the firewall OpenFirewall configures.
func FirewallBackend() string { return "Windows Firewall" }

// readRuleScript prints the powerhook rule's ports and remote addresses using
// the firewall cmdlets, whose values (unlike netsh's labels) aren't localized.
const readRuleScript = `$r = @(Get-NetFirewallRule -DisplayName 'powerhook' -ErrorAction Stop)
if ($r.Count -ne 1) { exit 2 }
"enabled=$($r[0].Enabled)"
foreach ($p in @(($r[0] | Get-NetFirewallPortFilter).LocalPort)) { "port=$p" }
foreach ($a in @(($r[0] | Get-NetFirewallAddressFilter).RemoteAddress)) { "remote=$a" }`

// FirewallCurrent reports whether the powerhook rule already allows exactly
// cfg's port from cfg's allowed_ips, read back from Windows Firewall.
func FirewallCurrent(cfg *config.Config) bool {
	out, err := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", readRuleScript).Output()
	if err != nil {
		return false
	}
	var ports []string
	var found []netip.Prefix
	enabled := false
	for _, line := range strings.Split(string(out), "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		switch k {
		case "enabled":
			enabled = strings.EqualFold(v, "True")
		case "port":
			ports = append(ports, v)
		case "remote":
			p, ok := parseWindowsRemote(v)
			if !ok {
				return false
			}
			found = append(found, p)
		}
	}
	if !enabled || len(ports) != 1 || ports[0] != cfg.Port() {
		return false
	}
	// OpenFirewall uses "any" as soon as one range covers everything.
	want := cfg.Allowed
	for _, p := range cfg.Allowed {
		if p.Bits() == 0 {
			want = []netip.Prefix{anyV4}
			break
		}
	}
	return samePrefixes(want, found)
}

func FirewallInstalled() string {
	if exec.Command("netsh", "advfirewall", "firewall", "show", "rule", "name="+fwName).Run() == nil {
		return "Windows Firewall"
	}
	return ""
}

func CloseFirewall() error {
	if FirewallInstalled() == "" {
		return nil
	}
	out, err := exec.Command("netsh", "advfirewall", "firewall", "delete", "rule", "name="+fwName).CombinedOutput()
	if err != nil {
		return fmt.Errorf("delete firewall rule: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}
