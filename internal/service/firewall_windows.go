//go:build windows

package service

import (
	"fmt"
	"log"
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
