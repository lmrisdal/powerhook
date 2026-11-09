package runner

import (
	"io"
	"log"
	"os"
	"runtime"
	"strings"
	"testing"

	"powerhook/internal/config"
)

func TestMain(m *testing.M) {
	log.SetOutput(io.Discard)
	os.Exit(m.Run())
}

func TestRun(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses /bin/sh")
	}
	if err := Run(config.Command{Exec: []string{"/bin/sh", "-c", "exit 0"}}); err != nil {
		t.Errorf("success: %v", err)
	}
	if err := Run(config.Command{Exec: []string{"/bin/sh", "-c", "exit 3"}}); err == nil {
		t.Error("exit 3 reported no error")
	}
	// No request data and no service environment reach the command.
	t.Setenv("POWERHOOK_SECRET", "x")
	err := Run(config.Command{Exec: []string{"/bin/sh", "-c", `test -z "$POWERHOOK_SECRET"`}})
	if err != nil {
		t.Errorf("environment leaked: %v", err)
	}
	err = Run(config.Command{Exec: []string{"/bin/sleep", "5"}, TimeoutS: 1})
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Errorf("timeout: got %v", err)
	}
}
