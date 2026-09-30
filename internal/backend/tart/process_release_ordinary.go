//go:build !n1diagnostic || n1candidate

package tart

import "fmt"

func processReleaseFailure(pid int, err error) error {
	return fmt.Errorf("release owned Tart process %d: %w", pid, err)
}
