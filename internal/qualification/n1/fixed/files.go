package fixed

import (
	"errors"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/contract"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/pathmeta"
	"io"
	"os"
	"path/filepath"
	"syscall"
)

var ErrRefused = errors.New("n1 fixed input refused")

type Inputs struct {
	Catalogue           contract.Catalogue
	Protected           contract.ProtectedInventory
	Build               contract.BuildInputs
	Lock                contract.StaticLock
	Handoff             contract.Handoff
	LockSHA, HandoffSHA string
}

func ReadInputs() (Inputs, error) {
	var v Inputs
	static, e := ReadStatic()
	if e != nil {
		return v, ErrRefused
	}
	v.Lock = static.Lock
	v.LockSHA = static.LockSHA
	v.Catalogue = static.Catalogue
	v.Protected = static.Protected
	v.Build = static.Build
	if checkDirectory(contract.EvidenceRoot, 501, 0700) != nil {
		return v, ErrRefused
	}
	raw, e := read(contract.EvidenceRoot+"/"+contract.HandoffName, contract.MaxReceiptBytes, 501, 0600, -1)
	if e != nil {
		return v, e
	}
	v.Handoff, e = contract.ParseHandoff(raw)
	if e != nil || v.Handoff.Window.LockSHA != v.LockSHA {
		return Inputs{}, ErrRefused
	}
	v.HandoffSHA = contract.SHA(raw)
	for i, n := range []string{"stock.enrolled.json", "candidate.enrolled.json"} {
		raw, e = read(contract.ConfigRoot+"/"+n, 4096, 501, 0600, -1)
		if e != nil || contract.SHA(raw) != v.Handoff.Configs[i] {
			return Inputs{}, ErrRefused
		}
	}
	return v, nil
}
func ReadCompletion() (contract.Completion, string, error) {
	raw, e := read(contract.EvidenceRoot+"/"+contract.CompletionName, contract.MaxReceiptBytes, 501, 0600, 501)
	if e != nil {
		return contract.Completion{}, "", e
	}
	c, e := contract.ParseCompletion(raw)
	return c, contract.SHA(raw), e
}
func CheckArtifact(index int, lock contract.StaticLock) error {
	p := contract.ArtifactPath(index)
	if p == "" {
		return ErrRefused
	}
	raw, e := read(p, 128<<20, 501, 0500, -1)
	if e != nil || contract.SHA(raw) != lock.Artifacts[index].SHA {
		return ErrRefused
	}
	return nil
}
func checkDirectory(path string, uid int, mode os.FileMode) error {
	before, e := os.Lstat(path)
	if e != nil || !before.IsDir() || before.Mode() != os.ModeDir|mode {
		return ErrRefused
	}
	st, ok := before.Sys().(*syscall.Stat_t)
	if !ok || int(st.Uid) != uid || pathmeta.Check(path, before, pathmeta.OSInspector{}) != nil {
		return ErrRefused
	}
	canonical, e := filepath.EvalSymlinks(path)
	if e != nil || canonical != path {
		return ErrRefused
	}
	return nil
}
func ancestry(path string) error {
	for p := filepath.Dir(path); ; p = filepath.Dir(p) {
		info, e := os.Lstat(p)
		if e != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0022 != 0 {
			return ErrRefused
		}
		st, ok := info.Sys().(*syscall.Stat_t)
		if !ok || (st.Uid != 0 && st.Uid != 501) || pathmeta.CheckQualificationAncestor(p, info, pathmeta.OSInspector{}, CheckPlatform) != nil {
			return ErrRefused
		}
		if p == "/" {
			return nil
		}
	}
}

type fileReadHandle interface {
	io.Reader
	Stat() (os.FileInfo, error)
	Close() error
}
type fileReadChecks struct {
	ancestry func(string) error
	stat     func(string) (os.FileInfo, error)
	acl      func(string, os.FileInfo) error
	open     func(string) (fileReadHandle, error)
}

func read(path string, cap, uid int, mode os.FileMode, gid int) ([]byte, error) {
	return readChecked(path, cap, uid, mode, gid, fileReadChecks{ancestry, os.Lstat, func(p string, f os.FileInfo) error { return pathmeta.Check(p, f, pathmeta.OSInspector{}) }, func(p string) (fileReadHandle, error) {
		fd, e := syscall.Open(p, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
		if e != nil {
			return nil, e
		}
		return os.NewFile(uintptr(fd), "n1-fixed-input"), nil
	}})
}
func readChecked(path string, cap, uid int, mode os.FileMode, gid int, i fileReadChecks) (raw []byte, err error) {
	if i.ancestry == nil || i.stat == nil || i.acl == nil || i.open == nil || cap < 1 || i.ancestry(path) != nil {
		return nil, ErrRefused
	}
	before, e := i.stat(path)
	if e != nil || before == nil || !before.Mode().IsRegular() || before.Mode() != mode || before.Size() < 1 || before.Size() > int64(cap) {
		return nil, ErrRefused
	}
	st, ok := before.Sys().(*syscall.Stat_t)
	if !ok || int(st.Uid) != uid || st.Nlink != 1 || gid >= 0 && int(st.Gid) != gid || i.acl(path, before) != nil {
		return nil, ErrRefused
	}
	f, e := i.open(path)
	if e != nil || f == nil {
		return nil, ErrRefused
	}
	defer func() {
		if f.Close() != nil {
			raw = nil
			err = ErrRefused
		}
	}()
	opened, e := f.Stat()
	if e != nil || !same(before, opened) {
		return nil, ErrRefused
	}
	raw, e = io.ReadAll(io.LimitReader(f, int64(cap)+1))
	if e != nil || len(raw) > cap {
		return nil, ErrRefused
	}
	visible, e := i.stat(path)
	after, e2 := f.Stat()
	if e != nil || e2 != nil || !same(opened, visible) || !same(opened, after) || i.ancestry(path) != nil || i.acl(path, visible) != nil {
		return nil, ErrRefused
	}
	// The ACL query is pathname-based: bracket its complete leaf security tuple
	// against the retained descriptor and the original opened identity.
	finalVisible, ve := i.stat(path)
	finalOpened, fe := f.Stat()
	if ve != nil || fe != nil || !same(opened, finalVisible) || !same(opened, finalOpened) {
		return nil, ErrRefused
	}
	return raw, nil
}
func same(a, b os.FileInfo) bool {
	if a == nil || b == nil || !os.SameFile(a, b) || a.Mode() != b.Mode() || a.Size() != b.Size() || !a.ModTime().Equal(b.ModTime()) {
		return false
	}
	x, xo := a.Sys().(*syscall.Stat_t)
	y, yo := b.Sys().(*syscall.Stat_t)
	return xo && yo && x.Dev == y.Dev && x.Ino == y.Ino && x.Uid == y.Uid && x.Gid == y.Gid && x.Mode == y.Mode && x.Nlink == y.Nlink && sameNativeSecurity(x, y)
}

func CheckStateDirectory() error {
	if checkDirectory(contract.StateRoot, 501, 0700) != nil || ancestry(contract.StateRoot+"/.") != nil {
		return ErrRefused
	}
	return nil
}

func CheckEvidenceDirectory() error {
	if checkDirectory(contract.EvidenceRoot, 501, 0700) != nil || ancestry(contract.EvidenceRoot+"/"+contract.CompletionName) != nil {
		return ErrRefused
	}
	return nil
}
