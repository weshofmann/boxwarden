//go:build linux

package main

import (
	"os"
	"syscall"
	"unsafe"
)

func clipboardTerminal(f *os.File) bool {
	var term syscall.Termios
	_, _, err := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), syscall.TCGETS, uintptr(unsafe.Pointer(&term)))
	return err == 0
}
