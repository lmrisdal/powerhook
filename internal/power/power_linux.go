package power

import (
	"fmt"
	"os/exec"
	"strings"
)

// linux uses systemctl, which asks systemd-logind to do the work. Running as
// root (the systemd service), this needs no polkit prompt.
type linux struct{}

func newNative(Options) Controller { return linux{} }

func (linux) Suspend() error  { return systemctl("suspend") }
func (linux) Poweroff() error { return systemctl("poweroff") }
func (linux) Restart() error  { return systemctl("reboot") }

func systemctl(verb string) error {
	out, err := exec.Command("systemctl", verb).CombinedOutput()
	if err != nil {
		return fmt.Errorf("systemctl %s: %w: %s", verb, err, strings.TrimSpace(string(out)))
	}
	return nil
}
