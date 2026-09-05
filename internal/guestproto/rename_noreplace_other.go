//go:build !linux

package guestproto

import (
	"fmt"
	"os"
)

func renameWithoutReplacement(source, destination string) error {
	if _, err := os.Lstat(destination); err == nil {
		return fmt.Errorf("destination already exists")
	} else if !os.IsNotExist(err) {
		return err
	}
	return os.Rename(source, destination)
}
