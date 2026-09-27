//go:build !darwin

// Package renamex publishes owner-private state without replacing an existing name.
package renamex

import (
	"errors"
	"fmt"
	"os"
)

// Other platforms are test-only for the Tart-backed alpha. Production Darwin
// uses an atomic no-replace rename; callers serialize this private parent.
func NoReplace(parent *os.Root, from, to string) error {
	if _, err := parent.Lstat(to); !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("destination changed: %v", err)
	}
	return parent.Rename(from, to)
}
