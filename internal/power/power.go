// Package power performs suspend, poweroff and restart on the host OS.
package power

import "log"

// Controller performs power actions.
type Controller interface {
	Suspend() error
	Poweroff() error
	Restart() error
}

// Options tune the native controller.
type Options struct {
	// ForceCloseApps closes apps that would block shutdown (Windows only).
	ForceCloseApps bool
}

// New returns the controller for this OS.
func New(opts Options) Controller { return newNative(opts) }

// DryRun logs actions instead of performing them.
type DryRun struct{}

func (DryRun) Suspend() error  { log.Print("dry run: would suspend"); return nil }
func (DryRun) Poweroff() error { log.Print("dry run: would power off"); return nil }
func (DryRun) Restart() error  { log.Print("dry run: would restart"); return nil }
