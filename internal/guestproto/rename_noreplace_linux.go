//go:build linux

package guestproto

import (
	"syscall"
	"unsafe"
)

const (
	atFDCWD         = ^uintptr(99) // Linux AT_FDCWD (-100) in unsigned syscall form.
	renameNoReplace = 1
)

// renameWithoutReplacement uses Linux renameat2(RENAME_NOREPLACE), rather
// than trusting a check-then-rename window around the active trust directory.
func renameWithoutReplacement(source, destination string) error {
	from, err := syscall.BytePtrFromString(source)
	if err != nil {
		return err
	}
	to, err := syscall.BytePtrFromString(destination)
	if err != nil {
		return err
	}
	_, _, errno := syscall.Syscall6(syscall.SYS_RENAMEAT2,
		atFDCWD, uintptr(unsafe.Pointer(from)), atFDCWD, uintptr(unsafe.Pointer(to)), renameNoReplace, 0)
	if errno != 0 {
		return errno
	}
	return nil
}
