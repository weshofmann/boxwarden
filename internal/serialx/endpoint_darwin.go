package serialx

import (
	"os"
	"syscall"
)

func openEndpointLink(path string) (*os.File, error) {
	// O_SYMLINK opens the symlink inode instead of following its PTY target.
	fd, err := syscall.Open(path, syscall.O_SYMLINK|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), path), nil
}
