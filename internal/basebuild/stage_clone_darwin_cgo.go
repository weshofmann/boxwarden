//go:build darwin && cgo

package basebuild

/*
#include <fcntl.h>
#include <stdlib.h>
#include <sys/clonefile.h>
*/
import "C"

import (
	"errors"
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

// cloneStagedFile uses the already-open, checked source descriptor. APFS
// shares its immutable ISO blocks until either file changes; the caller still
// rehashes the staged bytes against the preparation key before running them.
func cloneStagedFile(source *os.File, target string, mode os.FileMode) (bool, error) {
	info, err := source.Stat()
	if err != nil {
		return false, err
	}
	metadata, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return false, errors.New("cannot inspect installer file flags")
	}
	if metadata.Flags != 0 {
		return false, nil // A clone inherits flags such as UF_IMMUTABLE; copy safe bytes instead.
	}
	cTarget := C.CString(target)
	defer C.free(unsafe.Pointer(cTarget))
	result, callErr := C.fclonefileat(C.int(source.Fd()), C.AT_FDCWD, cTarget, C.CLONE_NOOWNERCOPY|C.CLONE_NOFOLLOW_ANY)
	if result != 0 {
		if errors.Is(callErr, syscall.ENOTSUP) || errors.Is(callErr, syscall.EXDEV) || errors.Is(callErr, syscall.ENOSYS) || errors.Is(callErr, syscall.EINVAL) {
			if _, err := os.Lstat(target); errors.Is(err, os.ErrNotExist) {
				return false, nil // Filesystem cannot clone; use the bounded copy path.
			}
		}
		return false, fmt.Errorf("clone staged installer: %w", callErr)
	}
	entry, err := os.Lstat(target)
	if err != nil || !entry.Mode().IsRegular() {
		return false, errors.New("cloned installer has unsafe type")
	}
	file, err := os.OpenFile(target, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return false, err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(entry, opened) {
		return false, errors.New("cloned installer changed while opening")
	}
	if err := file.Chmod(mode); err != nil {
		return false, err
	}
	if err := file.Sync(); err != nil {
		return false, err
	}
	return true, nil
}
