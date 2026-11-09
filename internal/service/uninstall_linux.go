//go:build linux

package service

import (
	"fmt"
	"os"
	"path/filepath"

	"powerhook/internal/config"
)

// installedComponents lists what can be removed, in safe removal order.
func installedComponents() ([]component, error) {
	var comps []component

	if _, err := os.Stat(unitPath); err == nil {
		state := "disabled"
		if unitEnabled(unitName) {
			state = "enabled"
		}
		active := unitActive(unitName)
		if active {
			state += ", active"
		}
		comps = append(comps, component{
			name: fmt.Sprintf("systemd service %s (%s)", unitName, state),
			remove: func() error {
				if active {
					if err := runSystemctl("stop", unitName); err != nil {
						return err
					}
				}
				if err := runSystemctl("disable", unitName); err != nil {
					return err
				}
				if err := os.Remove(unitPath); err != nil && !os.IsNotExist(err) {
					return err
				}
				_ = runSystemctl("reset-failed", unitName)
				return runSystemctl("daemon-reload")
			},
		})
	}

	if fw := FirewallInstalled(); fw != "" {
		comps = append(comps, component{name: "firewall rules (" + fw + ")", remove: CloseFirewall})
	}

	self, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("locate current binary: %w", err)
	}
	if s, e := filepath.EvalSymlinks(self); e == nil {
		self = s
	}
	comps = append(comps, component{
		name: "binary: " + self,
		remove: func() error {
			if err := os.Remove(self); err != nil && !os.IsNotExist(err) {
				return err
			}
			return nil
		},
	})

	if dirExists(config.DefaultDir) {
		comps = append(comps, component{
			name:   "config: " + config.DefaultDir,
			remove: func() error { return os.RemoveAll(config.DefaultDir) },
		})
	}
	return comps, nil
}
