//go:build darwin

package importx

import (
	"os"
	"syscall"
	"unsafe"
)

// renameExclusive publishes a complete snapshot without replacing another
// transaction's destination. Darwin's renameatx_np supports RENAME_EXCL.
func renameExclusive(parent *os.Root, from, to string) error {
	directory, err := parent.Open(".")
	if err != nil {
		return err
	}
	defer directory.Close()
	fromName, err := syscall.BytePtrFromString(from)
	if err != nil {
		return err
	}
	toName, err := syscall.BytePtrFromString(to)
	if err != nil {
		return err
	}
	// SYS_renameatx_np=488 and RENAME_EXCL=0x4 in the macOS SDK.
	_, _, errno := syscall.Syscall6(488, directory.Fd(), uintptr(unsafe.Pointer(fromName)), directory.Fd(), uintptr(unsafe.Pointer(toName)), 0x4, 0)
	if errno != 0 {
		return errno
	}
	return nil
}
