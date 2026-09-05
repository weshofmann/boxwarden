//go:build darwin

package hostx

import (
	"syscall"
	"unsafe"
)

const (
	sysRenameatxNP      = 488
	renameExclusiveFlag = 0x00000004
)

func renameExclusive(oldPath, newPath string) error {
	oldPointer, err := syscall.BytePtrFromString(oldPath)
	if err != nil {
		return err
	}
	newPointer, err := syscall.BytePtrFromString(newPath)
	if err != nil {
		return err
	}
	atFDCWD := ^uintptr(1)
	_, _, errno := syscall.Syscall6(sysRenameatxNP, atFDCWD, uintptr(unsafe.Pointer(oldPointer)), atFDCWD, uintptr(unsafe.Pointer(newPointer)), renameExclusiveFlag, 0)
	if errno != 0 {
		return errno
	}
	return nil
}
