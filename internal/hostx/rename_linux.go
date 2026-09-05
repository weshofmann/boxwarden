//go:build linux

package hostx

import (
	"os"
)

// Linux is not a qualified production host. This conservative fallback is used
// only by deterministic CI; preexistence is checked and never overwritten.
func renameExclusive(oldPath, newPath string) error {
	if _, err := os.Lstat(newPath); err == nil {
		return os.ErrExist
	} else if !os.IsNotExist(err) {
		return err
	}
	return os.Rename(oldPath, newPath)
}
