//go:build linux && arm64

package guestproto

import (
	"syscall"
	"unsafe"
)

const (
	atFDCWD               = ^uintptr(99)
	renameNoReplace       = 1
	linuxRenameat2Syscall = syscall.SYS_RENAMEAT2
)

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
