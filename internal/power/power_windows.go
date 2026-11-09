package power

import (
	"fmt"

	"golang.org/x/sys/windows"
)

var (
	powrprof            = windows.NewLazySystemDLL("powrprof.dll")
	procSetSuspendState = powrprof.NewProc("SetSuspendState")
)

// SHTDN_REASON_FLAG_PLANNED | SHTDN_REASON_MAJOR_OTHER | SHTDN_REASON_MINOR_OTHER
const shutdownReason = 0x80000000

type win struct{ force bool }

func newNative(o Options) Controller { return win{force: o.ForceCloseApps} }

func (w win) Suspend() error {
	if err := enableShutdownPrivilege(); err != nil {
		return err
	}
	// SetSuspendState(bHibernate=FALSE, bForce=FALSE, bWakeupEventsDisabled=FALSE)
	r, _, err := procSetSuspendState.Call(0, 0, 0)
	if r == 0 {
		return fmt.Errorf("SetSuspendState: %w", err)
	}
	return nil
}

func (w win) Poweroff() error { return w.shutdown(false) }
func (w win) Restart() error  { return w.shutdown(true) }

// shutdown does a full shutdown (not a Fast Startup hybrid one), which keeps
// NTFS partitions clean for the Linux side of a dual boot.
func (w win) shutdown(reboot bool) error {
	if err := enableShutdownPrivilege(); err != nil {
		return err
	}
	msg, _ := windows.UTF16PtrFromString("Powerhook: remote power request")
	if err := windows.InitiateSystemShutdownEx(nil, msg, 0, w.force, reboot, shutdownReason); err != nil {
		return fmt.Errorf("InitiateSystemShutdownEx: %w", err)
	}
	return nil
}

func enableShutdownPrivilege() error {
	var token windows.Token
	if err := windows.OpenProcessToken(windows.CurrentProcess(), windows.TOKEN_ADJUST_PRIVILEGES|windows.TOKEN_QUERY, &token); err != nil {
		return fmt.Errorf("OpenProcessToken: %w", err)
	}
	defer token.Close()
	var luid windows.LUID
	name, _ := windows.UTF16PtrFromString("SeShutdownPrivilege")
	if err := windows.LookupPrivilegeValue(nil, name, &luid); err != nil {
		return fmt.Errorf("LookupPrivilegeValue: %w", err)
	}
	tp := windows.Tokenprivileges{PrivilegeCount: 1}
	tp.Privileges[0] = windows.LUIDAndAttributes{Luid: luid, Attributes: windows.SE_PRIVILEGE_ENABLED}
	if err := windows.AdjustTokenPrivileges(token, false, &tp, 0, nil, nil); err != nil {
		return fmt.Errorf("AdjustTokenPrivileges: %w", err)
	}
	return nil
}
