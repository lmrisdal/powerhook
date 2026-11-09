// Package runner runs the commands defined in the config.
package runner

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"powerhook/internal/config"
)

const maxLoggedOutput = 4 << 10

// Run executes c without a shell, waits for it, and logs its output. It returns
// an error if the command fails or exceeds its timeout.
func Run(c config.Command) error {
	ctx, cancel := context.WithTimeout(context.Background(), c.Timeout())
	defer cancel()

	cmd := exec.CommandContext(ctx, c.Exec[0], c.Exec[1:]...)
	cmd.Env = environment()
	// If the command leaves children holding its output pipe after a kill,
	// don't wait on them forever.
	cmd.WaitDelay = 2 * time.Second
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out

	start := time.Now()
	err := cmd.Run()
	if text := tail(out.String()); text != "" {
		log.Printf("command %s output:\n%s", c.Exec[0], text)
	}
	if ctx.Err() == context.DeadlineExceeded {
		return fmt.Errorf("%s timed out after %s", c.Exec[0], c.Timeout())
	}
	if err != nil {
		return fmt.Errorf("%s: %w", c.Exec[0], err)
	}
	log.Printf("command %s finished in %s", c.Exec[0], time.Since(start).Round(time.Millisecond))
	return nil
}

// environment is a minimal one on Unix, so nothing from the service's
// surroundings leaks in. Windows programs need SystemRoot and friends, so they
// inherit it.
func environment() []string {
	if runtime.GOOS == "windows" {
		return os.Environ()
	}
	return []string{"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin", "LANG=C.UTF-8"}
}

func tail(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > maxLoggedOutput {
		s = "..." + s[len(s)-maxLoggedOutput:]
	}
	return s
}
