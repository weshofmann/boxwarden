//go:build !darwin

package tart

import (
	"fmt"
	"os/exec"
)

func configureOwnedProcessGroup(*exec.Cmd) {}

func signalOwnedProcessGroup(int) error {
	return fmt.Errorf("owned process groups are unsupported on this platform")
}
