//go:build windows

package service

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"powerhook/internal/config"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

func IsService() bool {
	ok, err := svc.IsWindowsService()
	return err == nil && ok
}

type serviceHandler struct{ run func(context.Context) error }

func (h serviceHandler) Execute(_ []string, req <-chan svc.ChangeRequest, st chan<- svc.Status) (bool, uint32) {
	st <- svc.Status{State: svc.StartPending}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errc := make(chan error, 1)
	go func() { errc <- h.run(ctx) }()
	st <- svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown}
	for {
		select {
		case c := <-req:
			switch c.Cmd {
			case svc.Interrogate:
				st <- c.CurrentStatus
			case svc.Stop, svc.Shutdown:
				st <- svc.Status{State: svc.StopPending}
				cancel()
				<-errc
				return false, 0
			}
		case err := <-errc:
			if err != nil {
				log.Printf("server stopped: %v", err)
				return true, 1
			}
			return false, 0
		}
	}
}

// runService runs fn under the Windows service manager until stopped.
func Run(fn func(context.Context) error) error {
	return svc.Run(Name, serviceHandler{run: fn})
}

// openService returns the powerhook service, or nil if it isn't installed.
func openService() (*mgr.Mgr, *mgr.Service, error) {
	m, err := mgr.Connect()
	if err != nil {
		return nil, nil, fmt.Errorf("connect to service manager: %w", err)
	}
	s, err := m.OpenService(Name)
	if err != nil {
		if errors.Is(err, windows.ERROR_SERVICE_DOES_NOT_EXIST) {
			return m, nil, nil
		}
		m.Disconnect()
		return nil, nil, err
	}
	return m, s, nil
}

func serviceRunning(s *mgr.Service) bool {
	st, err := s.Query()
	return err == nil && st.State != svc.Stopped
}

func stopService(s *mgr.Service) error {
	if !serviceRunning(s) {
		return nil
	}
	if _, err := s.Control(svc.Stop); err != nil && !errors.Is(err, windows.ERROR_SERVICE_NOT_ACTIVE) {
		return fmt.Errorf("stop service: %w", err)
	}
	for deadline := time.Now().Add(15 * time.Second); time.Now().Before(deadline); time.Sleep(300 * time.Millisecond) {
		if !serviceRunning(s) {
			return nil
		}
	}
	return errors.New("timed out waiting for the service to stop")
}

func RestartIfRunning() (bool, error) {
	m, s, err := openService()
	if err != nil {
		return false, err
	}
	defer m.Disconnect()
	if s == nil {
		return false, nil
	}
	defer s.Close()
	if !serviceRunning(s) {
		return false, nil
	}
	if err := stopService(s); err != nil {
		return false, err
	}
	return true, s.Start()
}

// AdminHint is appended to permission errors.
const AdminHint = "run it from an Administrator terminal"

// installDir is where packaging/install.ps1 puts the exe.
var installDir = filepath.Join(envOr("ProgramFiles", `C:\Program Files`), "powerhook")

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// RequireAdmin fails unless running elevated.
func RequireAdmin(cmd string) error {
	if !windows.GetCurrentProcessToken().IsElevated() {
		return fmt.Errorf("%s needs Administrator rights; %s", cmd, AdminHint)
	}
	return nil
}

// OpenLog returns the log file used under the service manager (there is no
// console), truncating it first if it has grown past 5 MB.
func OpenLog() (*os.File, error) {
	path := filepath.Join(config.DefaultDir, "powerhook.log")
	flags := os.O_CREATE | os.O_APPEND | os.O_WRONLY
	if st, err := os.Stat(path); err == nil && st.Size() > 5<<20 {
		flags |= os.O_TRUNC
	}
	return os.OpenFile(path, flags, 0o600)
}
