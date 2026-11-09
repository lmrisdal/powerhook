//go:build !linux && !windows

package sysinfo

import "runtime"

func osRelease() (id, name string) { return runtime.GOOS, runtime.GOOS }
func uptime() int64                { return 0 }
