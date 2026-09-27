//go:build linux && arm64

package exportx

import (
	"syscall"
	"unsafe"
)

const linuxRenameat2 = 276 // Linux arm64 renameat2(2).

func renameNoReplace(parentFD int, from, to string) error {
	oldName, err := syscall.BytePtrFromString(from)
	if err != nil {
		return err
	}
	newName, err := syscall.BytePtrFromString(to)
	if err != nil {
		return err
	}
	_, _, errno := syscall.Syscall6(linuxRenameat2, uintptr(parentFD), uintptr(unsafe.Pointer(oldName)), uintptr(parentFD), uintptr(unsafe.Pointer(newName)), 1, 0)
	if errno != 0 {
		return errno
	}
	return nil
}
