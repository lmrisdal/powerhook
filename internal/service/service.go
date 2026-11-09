// Package service integrates powerhook with the OS service manager (systemd on
// Linux, the Windows service manager on Windows) and the firewall, and
// implements the interactive uninstaller.
package service

// Name is the service name on every OS.
const Name = "powerhook"
