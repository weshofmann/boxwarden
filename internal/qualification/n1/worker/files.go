//go:build n1clipboarddiagnostic && !n1candidate

package worker

import (
	"io"
	"os"
	"path/filepath"
	"syscall"

	"github.com/weshofmann/boxwarden/internal/qualification/n1/fixed"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/pathmeta"
)

type FileMetadata struct {
	Path    string `json:"path"`
	Device  uint64 `json:"device"`
	Inode   uint64 `json:"inode"`
	UID     uint32 `json:"uid"`
	GID     uint32 `json:"gid"`
	Mode    uint32 `json:"mode"`
	Links   uint64 `json:"links"`
	Size    int64  `json:"size"`
	MtimeNS int64  `json:"mtime_ns"`
	CtimeNS int64  `json:"ctime_ns"`
	Flags   uint32 `json:"flags"`
}

func metadata(path string, mode os.FileMode) (FileMetadata, error) {
	return metadataWith(path, mode, pathmeta.OSInspector{})
}
func metadataWith(path string, mode os.FileMode, inspector interface{ HasExtendedACL(string) (bool, error) }) (FileMetadata, error) {
	info, e := os.Lstat(path)
	if e != nil || !info.Mode().IsRegular() || info.Mode() != mode || info.Size() < 1 {
		return FileMetadata{}, ErrRefused
	}
	s, ok := info.Sys().(*syscall.Stat_t)
	if !ok || s.Uid != 501 || s.Nlink != 1 {
		return FileMetadata{}, ErrRefused
	}
	canonical, e := filepath.EvalSymlinks(path)
	if e != nil || canonical != path {
		return FileMetadata{}, ErrRefused
	}
	type parent struct {
		path string
		info os.FileInfo
	}
	parents := []parent{}
	for p := filepath.Dir(path); ; p = filepath.Dir(p) {
		d, e := os.Lstat(p)
		if e != nil || !d.IsDir() || d.Mode()&os.ModeSymlink != 0 || d.Mode().Perm()&0022 != 0 {
			return FileMetadata{}, ErrRefused
		}
		st, ok := d.Sys().(*syscall.Stat_t)
		if !ok || (st.Uid != 0 && st.Uid != 501) {
			return FileMetadata{}, ErrRefused
		}
		if pathmeta.CheckQualificationAncestor(p, d, pathmeta.OSInspector{}, fixed.CheckPlatform) != nil {
			return FileMetadata{}, ErrRefused
		}
		parents = append(parents, parent{p, d})
		if p == "/" {
			break
		}
	}
	acl, e := inspector.HasExtendedACL(path)
	if e != nil || acl {
		return FileMetadata{}, ErrRefused
	}
	final, e := os.Lstat(path)
	canonical, ce := filepath.EvalSymlinks(path)
	if e != nil || ce != nil || canonical != path || stamp(path, final) != stamp(path, info) {
		return FileMetadata{}, ErrRefused
	}
	for _, p := range parents {
		d, e := os.Lstat(p.path)
		if e != nil || !sameParent(d, p.info) || pathmeta.CheckQualificationAncestor(p.path, d, pathmeta.OSInspector{}, fixed.CheckPlatform) != nil {
			return FileMetadata{}, ErrRefused
		}
		finalParent, e := os.Lstat(p.path)
		if e != nil || !sameParent(d, finalParent) {
			return FileMetadata{}, ErrRefused
		}
	}
	final, e = os.Lstat(path)
	canonical, ce = filepath.EvalSymlinks(path)
	if e != nil || ce != nil || canonical != path || stamp(path, final) != stamp(path, info) {
		return FileMetadata{}, ErrRefused
	}
	return stamp(path, info), nil
}
func stamp(path string, info os.FileInfo) FileMetadata {
	s, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return FileMetadata{}
	}
	ctime, flags := nativeMeta(s)
	return FileMetadata{path, uint64(s.Dev), s.Ino, s.Uid, s.Gid, uint32(s.Mode), uint64(s.Nlink), info.Size(), info.ModTime().UnixNano(), ctime, flags}
}
func readLeaf(path string, mode os.FileMode, limit int) (raw []byte, m FileMetadata, err error) {
	before, e := metadata(path, mode)
	if e != nil || before.Size > int64(limit) {
		return nil, m, ErrRefused
	}
	fd, e := syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if e != nil {
		return nil, m, ErrRefused
	}
	f := os.NewFile(uintptr(fd), "n1-public")
	defer func() {
		if f.Close() != nil {
			raw = nil
			err = ErrRefused
		}
	}()
	opened, e := f.Stat()
	if e != nil || stamp(path, opened) != before {
		return nil, m, ErrRefused
	}
	raw, e = io.ReadAll(io.LimitReader(f, int64(limit)+1))
	after, e2 := metadata(path, mode)
	final, e3 := f.Stat()
	if e != nil || e2 != nil || e3 != nil || len(raw) > limit || before != after || stamp(path, final) != before {
		return nil, m, ErrRefused
	}
	return raw, before, nil
}

// Directory entry updates change timestamps and size without changing ancestry
// authority. APFS directory nlink also counts ordinary entries; retain exact
// identity/owner/mode/flags and positive nlink. Leaves retain the full stamp.
func sameParent(a, b os.FileInfo) bool {
	x, xok := a.Sys().(*syscall.Stat_t)
	y, yok := b.Sys().(*syscall.Stat_t)
	if !xok || !yok {
		return false
	}
	_, xf := nativeMeta(x)
	_, yf := nativeMeta(y)
	return a.IsDir() && b.IsDir() && x.Dev == y.Dev && x.Ino == y.Ino && x.Uid == y.Uid && x.Gid == y.Gid && x.Mode == y.Mode && x.Nlink > 0 && y.Nlink > 0 && xf == yf
}
