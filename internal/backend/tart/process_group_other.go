//go:build !darwin

package tart

import (
	"fmt"
	"syscall"
)

func ownedProcessGroupAttributes() *syscall.SysProcAttr { return nil }

func supportsOwnedProcessGroups() bool { return false }

func signalOwnedProcessGroup(int) error {
	return fmt.Errorf("owned process groups are unsupported on this platform")
}

func requestOwnedProcessStop(int) error {
	return fmt.Errorf("guest stop requests are unsupported on this platform")
}
