//go:build !linux

package guestproto

import (
	"fmt"
	"os"
)

// Darwin is only a deterministic test host. Production guest binaries are
// Linux/arm64 and use renameat2(RENAME_NOREPLACE) above.
func renameWithoutReplacement(source, destination string) error {
	if _, err := os.Lstat(destination); err == nil {
		return fmt.Errorf("destination already exists")
	} else if !os.IsNotExist(err) {
		return err
	}
	return os.Rename(source, destination)
}
