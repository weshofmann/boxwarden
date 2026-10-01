//go:build darwin

package caller

import (
	"os"
	"syscall"

	"github.com/weshofmann/boxwarden/internal/qualification/n1/fixed"
)

// This function replaces L. Only standard descriptors and the unnamed fd3
// transition survive. An error return has no attendance dispatch authority.
func replaceImage(path string, argv, env []string, read *os.File) (err error) {
	err = fixed.ErrRefused
	ownsFD3 := false
	defer func() {
		if read != nil {
			if read.Close() != nil {
				err = fixed.ErrRefused
			}
		}
		if ownsFD3 {
			if syscall.Close(3) != nil {
				err = fixed.ErrRefused
			}
		}
	}()
	if read == nil {
		return fixed.ErrRefused
	}
	info, e := read.Stat()
	if e != nil || info.Mode()&os.ModeNamedPipe == 0 {
		return fixed.ErrRefused
	}
	if read.Fd() != 3 {
		if syscall.Dup2(int(read.Fd()), 3) != nil {
			return fixed.ErrRefused
		}
		ownsFD3 = true
		ce := read.Close()
		read = nil
		if ce != nil {
			return fixed.ErrRefused
		}
	}
	if _, _, errno := syscall.Syscall(syscall.SYS_FCNTL, 3, syscall.F_SETFD, 0); errno != 0 {
		return fixed.ErrRefused
	}
	if e := containInheritedDescriptors(); e != nil {
		return fixed.ErrRefused
	}
	if syscall.Exec(path, argv, env) != nil {
		return fixed.ErrRefused
	}
	return fixed.ErrRefused
}
