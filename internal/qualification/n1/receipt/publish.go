package receipt

import (
	"errors"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/contract"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/fixed"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/pathmeta"
	"io"
	"os"
	"path/filepath"
	"syscall"
)

var ErrRefused = fixed.ErrRefused

type publisher struct {
	dir      string
	uid, gid int
	acl      pathmeta.Inspector
	fail     func(string) error
}

func (p publisher) checkpoint(stage string) error {
	if p.fail != nil && p.fail(stage) != nil {
		return ErrRefused
	}
	return nil
}
func (p publisher) directory() (os.FileInfo, error) {
	canonical, e := filepath.EvalSymlinks(p.dir)
	if e != nil || canonical != p.dir {
		return nil, ErrRefused
	}
	info, e := os.Lstat(p.dir)
	if e != nil || info.Mode() != os.ModeDir|0700 {
		return nil, ErrRefused
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(st.Uid) != p.uid || pathmeta.Check(p.dir, info, p.acl) != nil {
		return nil, ErrRefused
	}
	return info, nil
}
func (p publisher) publish(c contract.Completion) error {
	if !c.Valid() {
		return ErrRefused
	}
	raw, e := contract.Encode(c)
	if e != nil {
		return ErrRefused
	}
	info, e := p.directory()
	if e != nil {
		return e
	}
	root, e := os.OpenRoot(p.dir)
	if e != nil {
		return ErrRefused
	}
	parent, e := root.Open(".")
	if e != nil {
		root.Close()
		return ErrRefused
	}
	parentCheck := func() error {
		visible, e := p.directory()
		pinned, e2 := parent.Stat()
		if e != nil || e2 != nil || !os.SameFile(info, visible) || !os.SameFile(info, pinned) || visible.Mode() != pinned.Mode() {
			return ErrRefused
		}
		return nil
	}
	if p.checkpoint("before-create") != nil || parentCheck() != nil {
		parent.Close()
		root.Close()
		return ErrRefused
	}
	f, e := root.OpenFile(contract.CompletionName, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		parent.Close()
		root.Close()
		return ErrRefused
	}
	operation := func() error {
		if p.checkpoint("after-create") != nil || f.Chown(p.uid, p.gid) != nil || p.checkpoint("after-chown") != nil || f.Chmod(0600) != nil || p.checkpoint("after-chmod") != nil {
			return ErrRefused
		}
		n, e := f.Write(raw)
		if e != nil || n != len(raw) || p.checkpoint("after-write") != nil || f.Sync() != nil || p.checkpoint("after-file-sync") != nil {
			return ErrRefused
		}
		if _, e = f.Seek(0, io.SeekStart); e != nil {
			return ErrRefused
		}
		back, e := io.ReadAll(io.LimitReader(f, contract.MaxReceiptBytes+1))
		if e != nil || string(back) != string(raw) || p.checkpoint("after-readback") != nil || p.file(f, len(raw)) != nil || parentCheck() != nil || parent.Sync() != nil || p.checkpoint("after-parent-sync") != nil {
			return ErrRefused
		}
		return nil
	}()
	closed := errors.Join(f.Close(), p.checkpoint("after-file-close"), parent.Close(), p.checkpoint("after-parent-close"), root.Close(), p.checkpoint("after-root-close"))
	if operation != nil || closed != nil {
		return ErrRefused
	}
	current, e := p.directory()
	if e != nil || !os.SameFile(info, current) {
		return ErrRefused
	}
	fd, e := syscall.Open(filepath.Join(p.dir, contract.CompletionName), syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if e != nil {
		return ErrRefused
	}
	f = os.NewFile(uintptr(fd), "n1-completion-readback")
	admitted := p.file(f, len(raw))
	back, re := io.ReadAll(io.LimitReader(f, contract.MaxReceiptBytes+1))
	after := p.file(f, len(raw))
	ce := f.Close()
	current, e = p.directory()
	if admitted != nil || after != nil || re != nil || ce != nil || e != nil || !os.SameFile(info, current) || string(back) != string(raw) || p.checkpoint("after-final-read") != nil {
		return ErrRefused
	}
	return nil
}
func (p publisher) file(f *os.File, size int) error {
	info, e := f.Stat()
	visible, e2 := os.Lstat(filepath.Join(p.dir, contract.CompletionName))
	if e != nil || e2 != nil || !same(info, visible) || info.Mode() != 0600 || info.Size() != int64(size) {
		return ErrRefused
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(st.Uid) != p.uid || int(st.Gid) != p.gid || st.Nlink != 1 || pathmeta.Check(filepath.Join(p.dir, contract.CompletionName), info, p.acl) != nil {
		return ErrRefused
	}
	return nil
}

func same(a, b os.FileInfo) bool {
	if !os.SameFile(a, b) || a.Mode() != b.Mode() || a.Size() != b.Size() || !a.ModTime().Equal(b.ModTime()) {
		return false
	}
	x, xo := a.Sys().(*syscall.Stat_t)
	y, yo := b.Sys().(*syscall.Stat_t)
	return xo && yo && x.Uid == y.Uid && x.Gid == y.Gid && x.Nlink == y.Nlink
}
func Publish(c contract.Completion) error {
	if !c.Valid() || os.Getuid() != 0 || os.Geteuid() != 0 || fixed.CheckEvidenceDirectory() != nil {
		return ErrRefused
	}
	p := publisher{dir: contract.EvidenceRoot, uid: 501, gid: 501, acl: pathmeta.OSInspector{}}
	if p.publish(c) != nil {
		return ErrRefused
	}
	_, sha, e := fixed.ReadCompletion()
	raw, _ := contract.Encode(c)
	if e != nil || sha != contract.SHA(raw) {
		return ErrRefused
	}
	return nil
}
