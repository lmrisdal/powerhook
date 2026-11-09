package config

import (
	"os"
	"path/filepath"
)

// DefaultDir holds the config installed by packaging/install.sh.
const DefaultDir = "/etc/powerhook"

// SearchPaths lists where Load looks when no path is given, in order.
func SearchPaths() []string {
	var paths []string
	if env := os.Getenv("POWERHOOK_CONFIG"); env != "" {
		paths = append(paths, env)
	}
	paths = append(paths, filepath.Join(DefaultDir, "config.json"))
	if home, err := os.UserHomeDir(); err == nil {
		paths = append(paths, filepath.Join(home, ".config", "powerhook", "config.json"))
	}
	return paths
}
