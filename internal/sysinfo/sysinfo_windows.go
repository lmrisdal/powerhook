package sysinfo

import (
	"fmt"

	"golang.org/x/sys/windows"
)

func osRelease() (id, name string) {
	v := windows.RtlGetVersion()
	product := "Windows 10"
	if v.MajorVersion >= 10 && v.BuildNumber >= 22000 {
		product = "Windows 11"
	}
	return "windows", fmt.Sprintf("%s (build %d)", product, v.BuildNumber)
}

func uptime() int64 { return int64(windows.DurationSinceBoot().Seconds()) }
