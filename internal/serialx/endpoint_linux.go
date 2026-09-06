package serialx

import (
	"os"
	"syscall"
)

func openEndpointLink(path string) (*os.File, error) {
	// O_PATH is not exposed by the frozen syscall package. This Linux ABI
	// flag with O_NOFOLLOW opens the symlink itself, retaining its inode.
	const oPath = 0x200000
	fd, err := syscall.Open(path, oPath|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), path), nil
}
