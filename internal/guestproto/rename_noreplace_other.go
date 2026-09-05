//go:build !linux

package guestproto

import "fmt"

func renameWithoutReplacement(source, destination string) error {
	return fmt.Errorf("atomic no-replace rename is unavailable on this platform")
}
