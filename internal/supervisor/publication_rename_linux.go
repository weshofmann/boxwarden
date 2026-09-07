//go:build linux

package supervisor

import "os"

// Linux is not a qualified Boxwarden runtime. This conservative CI fallback
// detects an already-present target before rename, exercising admission and
// collision handling but not claiming Darwin's kernel no-replace guarantee.
func renameWithoutReplace(source, destination string) error {
	if _, err := os.Lstat(destination); err == nil {
		return os.ErrExist
	} else if !os.IsNotExist(err) {
		return err
	}
	return os.Rename(source, destination)
}
