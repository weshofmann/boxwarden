//go:build darwin

package tart

import (
	"fmt"
	"os/exec"
	"syscall"
)

// configureOwnedProcessGroup separates Tart and its descendants from the
// supervisor. The direct child PID is therefore the group ID it owns.
func configureOwnedProcessGroup(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func supportsOwnedProcessGroups() bool { return true }

// signalOwnedProcessGroup sends Tart's graceful SIGINT only to the negative
// process group created for this direct child.
func signalOwnedProcessGroup(groupID int) error {
	if groupID >= 0 {
		return fmt.Errorf("owned process group ID must be negative")
	}
	return syscall.Kill(groupID, syscall.SIGINT)
}
