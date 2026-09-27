//go:build darwin

package exportx

import (
	"syscall"
	"unsafe"
)

const (
	macRenameatxNP = 488 // Darwin renameatx_np(2).
	macRenameExcl  = 0x4
)

func renameNoReplace(parentFD int, from, to string) error {
	oldName, err := syscall.BytePtrFromString(from)
	if err != nil {
		return err
	}
	newName, err := syscall.BytePtrFromString(to)
	if err != nil {
		return err
	}
	_, _, errno := syscall.Syscall6(macRenameatxNP, uintptr(parentFD), uintptr(unsafe.Pointer(oldName)), uintptr(parentFD), uintptr(unsafe.Pointer(newName)), macRenameExcl, 0)
	if errno != 0 {
		return errno
	}
	return nil
}
