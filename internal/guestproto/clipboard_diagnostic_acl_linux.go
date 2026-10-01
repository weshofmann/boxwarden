//go:build n1clipboarddiagnostic && linux

package guestproto

import (
	"github.com/weshofmann/boxwarden/internal/clipboarddiag"
	"syscall"
)

func diagnosticNoACL(path string, directory bool) error {
	keys := []string{"system.posix_acl_access"}
	if directory {
		keys = append(keys, "system.posix_acl_default")
	}
	for _, key := range keys {
		n, err := syscall.Getxattr(path, key, nil)
		if err == syscall.ENODATA || err == syscall.ENOTSUP {
			continue
		}
		if err != nil || n != 0 {
			return clipboarddiag.ErrMetadata
		}
	}
	return nil
}
