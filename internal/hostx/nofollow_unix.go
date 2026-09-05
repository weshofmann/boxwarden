//go:build darwin || linux

package hostx

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
)

func openNoFollow(path string) (*os.File, error) {
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), path), nil
}

// openVerifiedRegular binds validation and copying to the same open file
// description. Pre/post path identities catch ancestor replacement, while the
// fd digest prevents pathname replacement from changing admitted bytes.
func openVerifiedRegular(path, expectedDigest string, rejectPrivilege bool) (*os.File, error) {
	if !canonicalAbsolute(path) {
		return nil, fmt.Errorf("path must be canonical and absolute")
	}
	before, err := snapshotPath(path)
	if err != nil {
		return nil, err
	}
	file, err := openNoFollow(path)
	if err != nil {
		return nil, err
	}
	closeOnError := func(err error) (*os.File, error) { _ = file.Close(); return nil, err }
	info, err := file.Stat()
	if err != nil {
		return closeOnError(err)
	}
	if !info.Mode().IsRegular() || links(info) != 1 || (rejectPrivilege && info.Mode()&(os.ModeSetuid|os.ModeSetgid) != 0) {
		return closeOnError(fmt.Errorf("opened path is not a single-link unprivileged regular file"))
	}
	if !sameIdentity(before[len(before)-1], info) {
		return closeOnError(fmt.Errorf("path identity changed before open"))
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return closeOnError(err)
	}
	if expectedDigest != "" && fmt.Sprintf("%x", hash.Sum(nil)) != expectedDigest {
		return closeOnError(fmt.Errorf("file digest is unqualified"))
	}
	after, err := snapshotPath(path)
	if err != nil || !sameSnapshots(before, after) || !sameIdentity(after[len(after)-1], info) {
		return closeOnError(fmt.Errorf("path or ancestor identity changed during validation"))
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return closeOnError(err)
	}
	return file, nil
}

func snapshotPath(path string) ([]os.FileInfo, error) {
	components := []string{}
	for current := path; current != "/"; current = filepath.Dir(current) {
		components = append(components, current)
	}
	components = append(components, "/")
	result := make([]os.FileInfo, 0, len(components))
	for index := len(components) - 1; index >= 0; index-- {
		info, err := os.Lstat(components[index])
		if err != nil {
			return nil, err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("symlink component %q is prohibited", components[index])
		}
		if index != 0 && !info.IsDir() {
			return nil, fmt.Errorf("ancestor %q is not a directory", components[index])
		}
		result = append(result, info)
	}
	return result, nil
}

func sameSnapshots(a, b []os.FileInfo) bool {
	if len(a) != len(b) {
		return false
	}
	for index := range a {
		if !sameIdentity(a[index], b[index]) {
			return false
		}
	}
	return true
}
