//go:build !linux && !windows

package config

import "os"

const DefaultDir = "."

func SearchPaths() []string {
	if env := os.Getenv("POWERHOOK_CONFIG"); env != "" {
		return []string{env, "config.json"}
	}
	return []string{"config.json"}
}
