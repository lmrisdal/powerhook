// Package sysinfo reports which OS is running, for the /api/status endpoint.
package sysinfo

import (
	"os"
	"runtime"
)

// Info describes the running system.
type Info struct {
	Hostname      string `json:"hostname"`
	OS            string `json:"os"`      // runtime.GOOS: "linux" or "windows"
	OSID          string `json:"os_id"`   // e.g. "bazzite", "cachyos", "windows"
	OSName        string `json:"os_name"` // human-readable name
	UptimeSeconds int64  `json:"uptime_seconds"`
}

// Get gathers the current system info.
func Get() Info {
	host, _ := os.Hostname()
	id, name := osRelease()
	return Info{
		Hostname:      host,
		OS:            runtime.GOOS,
		OSID:          id,
		OSName:        name,
		UptimeSeconds: uptime(),
	}
}
