package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

const (
	unitName = "powerhook.service"
	unitPath = "/etc/systemd/system/powerhook.service"

	// AdminHint is appended to permission errors.
	AdminHint = "re-run with sudo"
)

// IsService is always false on Linux: systemd runs the binary in the foreground.
func IsService() bool { return false }

// Run is only meaningful under the Windows service manager.
func Run(func(context.Context) error) error { return errors.New("not a Windows service") }

// OpenLog is only used under the Windows service manager; journald captures
// stderr on Linux.
func OpenLog() (*os.File, error) { return nil, errors.New("not a Windows service") }

// RequireAdmin fails unless running as root.
func RequireAdmin(cmd string) error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("%s needs root; re-run: sudo powerhook %s", cmd, cmd)
	}
	return nil
}

// RestartIfRunning restarts the systemd unit if it is active.
func RestartIfRunning() (bool, error) {
	if !unitActive(unitName) {
		return false, nil
	}
	return true, runSystemctl("restart", unitName)
}

// Restart starts or restarts the systemd unit. installed is false when there
// is no unit to start.
func Restart() (installed bool, err error) {
	if _, err := os.Stat(unitPath); err != nil {
		return false, nil
	}
	return true, runSystemctl("restart", unitName)
}

func unitEnabled(u string) bool {
	out, _ := exec.Command("systemctl", "is-enabled", u).Output()
	return strings.TrimSpace(string(out)) == "enabled"
}

func unitActive(u string) bool {
	out, _ := exec.Command("systemctl", "is-active", u).Output()
	return strings.TrimSpace(string(out)) == "active"
}

func runSystemctl(args ...string) error {
	cmd := exec.Command("systemctl", args...)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("systemctl %s: %w", args[0], err)
	}
	return nil
}
