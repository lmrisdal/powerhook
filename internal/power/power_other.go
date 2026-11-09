//go:build !linux && !windows

package power

import (
	"errors"
	"runtime"
)

type unsupported struct{}

func newNative(Options) Controller { return unsupported{} }

var errUnsupported = errors.New("power actions are not supported on " + runtime.GOOS)

func (unsupported) Suspend() error  { return errUnsupported }
func (unsupported) Poweroff() error { return errUnsupported }
func (unsupported) Restart() error  { return errUnsupported }
