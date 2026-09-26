//go:build darwin

package tart

import (
	"fmt"
	"syscall"
)

// ownedProcessGroupAttributes separates Tart and its descendants from the
// supervisor. The direct child PID is therefore the group ID it owns.
func ownedProcessGroupAttributes() *syscall.SysProcAttr { return &syscall.SysProcAttr{Setpgid: true} }

func supportsOwnedProcessGroups() bool { return true }

// signalOwnedProcessGroup sends Tart's immediate-stop SIGINT only to the
// negative process group created for this direct child. Normal session stop
// first gives the guest a bounded SIGUSR2 shutdown request.
func signalOwnedProcessGroup(groupID int) error {
	if groupID >= 0 {
		return fmt.Errorf("owned process group ID must be negative")
	}
	return syscall.Kill(groupID, syscall.SIGINT)
}

// requestOwnedProcessStop addresses only the retained Tart child. Tart 2.32.1
// maps SIGUSR2 to VZVirtualMachine.requestStop(), which asks the guest OS to
// shut down. The caller still needs a bounded hard-stop fallback.
func requestOwnedProcessStop(processID int) error {
	if processID <= 0 {
		return fmt.Errorf("owned Tart process ID must be positive")
	}
	return syscall.Kill(processID, syscall.SIGUSR2)
}
