//go:build linux && amd64

package guestproto

import (
	"syscall"
	"unsafe"
)

const (
	atFDCWD               = ^uintptr(99)
	renameNoReplace       = 1
	linuxRenameat2Syscall = 316 // Linux x86_64 renameat2(2).
)

// renameWithoutReplacement uses the native Linux x86_64 renameat2 syscall.
// Go's syscall package does not expose SYS_RENAMEAT2 on amd64, so the audited
// architecture ABI number is kept narrowly here rather than using a fallback.
func renameWithoutReplacement(source, destination string) error {
	from, err := syscall.BytePtrFromString(source)
	if err != nil {
		return err
	}
	to, err := syscall.BytePtrFromString(destination)
	if err != nil {
		return err
	}
	_, _, errno := syscall.Syscall6(linuxRenameat2Syscall, atFDCWD, uintptr(unsafe.Pointer(from)), atFDCWD, uintptr(unsafe.Pointer(to)), renameNoReplace, 0)
	if errno != 0 {
		return errno
	}
	return nil
}
