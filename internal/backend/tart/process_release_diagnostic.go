//go:build n1diagnostic && !n1candidate

package tart

import (
	"errors"
	"fmt"
)

func processReleaseFailure(pid int, err error) error {
	return errors.Join(fmt.Errorf("release owned Tart process %d: %w", pid, err), ErrDiagnosticCleanupUnproven)
}
