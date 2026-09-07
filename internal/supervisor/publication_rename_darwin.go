//go:build darwin

package supervisor

import (
	"syscall"
	"unsafe"
)

const (
	sysRenameatxNP      = 488
	renameExclusiveFlag = 0x00000004
)

// renameWithoutReplace maps directly to Darwin renameatx_np(RENAME_EXCL).
// It remains available in native no-cgo builds.
func renameWithoutReplace(source, destination string) error {
	from, err := syscall.BytePtrFromString(source)
	if err != nil {
		return err
	}
	to, err := syscall.BytePtrFromString(destination)
	if err != nil {
		return err
	}
	const atFDCWD = ^uintptr(1)
	_, _, errno := syscall.Syscall6(sysRenameatxNP, atFDCWD, uintptr(unsafe.Pointer(from)), atFDCWD, uintptr(unsafe.Pointer(to)), renameExclusiveFlag, 0)
	if errno != 0 {
		return errno
	}
	return nil
}
