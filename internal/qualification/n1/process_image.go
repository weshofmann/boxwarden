//go:build (darwin || linux) && n1diagnostic && n1cleanup && !n1candidate

package n1

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"syscall"
)

type imageIdentity struct {
	sha           string
	device, inode uint64
}

func openProcessImage(path string) (*os.File, error) {
	fd, e := syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if e != nil {
		return nil, e
	}
	return os.NewFile(uintptr(fd), "n1-process-image"), nil
}
func readProcessImage(path string, open func(string) (*os.File, error)) (imageIdentity, error) {
	canonical, e := filepath.EvalSymlinks(path)
	if e != nil || canonical != path {
		return imageIdentity{}, ErrRefused
	}
	info, e := os.Lstat(path)
	if e != nil || !singleLinkImage(info) {
		return imageIdentity{}, ErrRefused
	}
	f, e := open(path)
	if e != nil {
		return imageIdentity{}, ErrRefused
	}
	opened, e := f.Stat()
	if e != nil || !singleLinkImage(opened) || !sameFile(info, opened) {
		f.Close()
		return imageIdentity{}, ErrRefused
	}
	hash := sha256.New()
	n, re := io.Copy(hash, io.LimitReader(f, 128<<20+1))
	current, se := f.Stat()
	ce := f.Close()
	visible, ve := os.Lstat(path)
	if re != nil || se != nil || ce != nil || ve != nil || n != info.Size() || !singleLinkImage(current) || !singleLinkImage(visible) || !sameFile(info, current) || !sameFile(info, visible) {
		return imageIdentity{}, ErrRefused
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return imageIdentity{}, ErrRefused
	}
	return imageIdentity{hex.EncodeToString(hash.Sum(nil)), uint64(st.Dev), st.Ino}, nil
}

// Exact one-link admission is independent of metadata equality: a stable
// hardlinked inode must refuse before open/hash as well as at final brackets.
func singleLinkImage(info os.FileInfo) bool {
	if info == nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > 128<<20 {
		return false
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	return ok && st.Nlink == 1
}
