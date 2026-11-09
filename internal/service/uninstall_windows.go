//go:build windows

package service

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"

	"powerhook/internal/config"
)

// installedComponents lists what can be removed, in safe removal order.
func installedComponents() ([]component, error) {
	var comps []component

	m, s, err := openService()
	if err != nil {
		return nil, err
	}
	defer m.Disconnect()
	if s != nil {
		state := "stopped"
		if serviceRunning(s) {
			state = "running"
		}
		s.Close()
		comps = append(comps, component{
			name: fmt.Sprintf("Windows service %s (%s)", Name, state),
			remove: func() error {
				m, s, err := openService()
				if err != nil {
					return err
				}
				defer m.Disconnect()
				if s == nil {
					return nil
				}
				defer s.Close()
				if err := stopService(s); err != nil {
					return err
				}
				return s.Delete()
			},
		})
	}

	if fw := FirewallInstalled(); fw != "" {
		comps = append(comps, component{name: "firewall rule (" + fw + ")", remove: CloseFirewall})
	}

	if dirExists(installDir) {
		comps = append(comps, component{
			name:   "program folder: " + installDir,
			remove: func() error { return removeAllOrOnReboot(installDir) },
		})
	}
	if dirExists(config.DefaultDir) {
		comps = append(comps, component{
			name:   "config: " + config.DefaultDir,
			remove: func() error { return os.RemoveAll(config.DefaultDir) },
		})
	}
	return comps, nil
}

// removeAllOrOnReboot deletes dir. Files that are in use (such as this
// running exe) are scheduled for deletion at the next reboot instead.
func removeAllOrOnReboot(dir string) error {
	if err := os.RemoveAll(dir); err == nil {
		return nil
	}
	var pending []string
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err == nil {
			pending = append(pending, p)
		}
		return nil
	})
	// Children before parents.
	for i := len(pending) - 1; i >= 0; i-- {
		p := pending[i]
		if os.Remove(p) == nil {
			continue
		}
		from, _ := windows.UTF16PtrFromString(p)
		if err := windows.MoveFileEx(from, nil, windows.MOVEFILE_DELAY_UNTIL_REBOOT); err != nil {
			return fmt.Errorf("remove %s: %w", p, err)
		}
	}
	if _, err := os.Stat(dir); err == nil {
		fmt.Printf("  %s is in use; it will be deleted at the next reboot.\n", dir)
	}
	return nil
}
