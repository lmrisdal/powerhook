package config

import (
	"os"
	"path/filepath"
)

// DefaultDir holds the config (and service log) installed by packaging/install.ps1.
var DefaultDir = filepath.Join(envOr("ProgramData", `C:\ProgramData`), "powerhook")

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// SearchPaths lists where Load looks when no path is given, in order.
func SearchPaths() []string {
	var paths []string
	if env := os.Getenv("POWERHOOK_CONFIG"); env != "" {
		paths = append(paths, env)
	}
	paths = append(paths, filepath.Join(DefaultDir, "config.json"))
	// %AppData%, the Windows equivalent of ~/.config on Linux.
	if dir, err := os.UserConfigDir(); err == nil {
		paths = append(paths, filepath.Join(dir, "powerhook", "config.json"))
	}
	return paths
}
