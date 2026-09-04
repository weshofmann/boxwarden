//go:build darwin || linux

package tart

import (
	"fmt"
	"os/exec"
	"syscall"
)

// configureOwnedProcessGroup separates Tart and its inherited Softnet children
// from the supervisor before exec. The process PID is then its group ID.
func configureOwnedProcessGroup(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// signalOwnedProcessGroup addresses only the negative PGID created for this
// owned child, so a shutdown cannot signal the supervisor's group or another
// generation. SIGINT retains Tart's normal attended shutdown behavior.
func signalOwnedProcessGroup(groupID int) error {
	if groupID >= 0 {
		return fmt.Errorf("owned process group ID must be negative")
	}
	return syscall.Kill(groupID, syscall.SIGINT)
}
