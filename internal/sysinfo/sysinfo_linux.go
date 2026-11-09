package sysinfo

import (
	"bufio"
	"os"
	"strconv"
	"strings"
)

func osRelease() (id, name string) {
	id, name = "linux", "Linux"
	f, err := os.Open("/etc/os-release")
	if err != nil {
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		k, v, ok := strings.Cut(sc.Text(), "=")
		if !ok {
			continue
		}
		v = strings.Trim(v, `"'`)
		switch k {
		case "ID":
			id = v
		case "PRETTY_NAME":
			name = v
		}
	}
	return
}

func uptime() int64 {
	b, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return 0
	}
	first, _, _ := strings.Cut(string(b), " ")
	f, _ := strconv.ParseFloat(first, 64)
	return int64(f)
}
