package fixed

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"github.com/weshofmann/boxwarden/internal/execx"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/pathmeta"
)

type sudoInspector struct {
	stat      func(string) (os.FileInfo, error)
	acl       func(string, os.FileInfo) error
	canonical func(string) (string, error)
	platform  func() error
}

// CheckSudo admits only the protected OS executable on the exact N1 trial
// platform. macOS sudo is root:wheel04511, unreadable by the operator. This is
// structural OS trust, not a digest/running-image attestation. H's separately
// qualified system-image census bindings remain mandatory and independent.
func CheckSudo() error {
	return checkSudo(sudoInspector{os.Lstat, func(p string, f os.FileInfo) error { return pathmeta.Check(p, f, pathmeta.OSInspector{}) }, filepath.EvalSymlinks, func() error {
		return sudoPlatform(execx.OSRunner{MaxOutputBytes: 512, StrictStderr: true}, runtime.GOOS, runtime.GOARCH)
	}})
}
func checkSudo(i sudoInspector) error {
	if i.stat == nil || i.acl == nil || i.canonical == nil || i.platform == nil {
		return ErrRefused
	}
	paths := []string{"/", "/usr", "/usr/bin", "/usr/bin/sudo"}
	before := make([]os.FileInfo, len(paths))
	inspect := func(p string, n int) (os.FileInfo, error) {
		f, e := i.stat(p)
		if e != nil || f == nil {
			return nil, ErrRefused
		}
		st, ok := f.Sys().(*syscall.Stat_t)
		if !ok || st.Uid != 0 || st.Gid != 0 {
			return nil, ErrRefused
		}
		if n == 3 {
			if !f.Mode().IsRegular() || f.Mode() != os.ModeSetuid|0511 || st.Nlink != 1 || f.Size() < 1 || f.Size() > 16<<20 {
				return nil, ErrRefused
			}
		} else if !f.IsDir() || f.Mode() != os.ModeDir|0755 {
			return nil, ErrRefused
		}
		canonical, e := i.canonical(p)
		if e != nil || canonical != p || i.acl(p, f) != nil {
			return nil, ErrRefused
		}
		return f, nil
	}
	for n, p := range paths {
		f, e := inspect(p, n)
		if e != nil {
			return ErrRefused
		}
		before[n] = f
	}
	if i.platform() != nil {
		return ErrRefused
	}
	for n, p := range paths {
		f, e := inspect(p, n)
		if e != nil || !sameSudo(before[n], f) {
			return ErrRefused
		}
	}
	return nil
}
func sudoPlatform(r execx.Runner, goos, goarch string) error {
	if r == nil || goos != "darwin" || goarch != "arm64" {
		return ErrRefused
	}
	for n, arg := range []string{"-productVersion", "-buildVersion"} {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		out, e := r.Run(ctx, execx.Command{Path: "/usr/bin/sw_vers", Args: []string{arg}, Env: []string{"LC_ALL=C", "LANG=C"}, Stdin: []byte{}})
		cancel()
		expected := []string{"27.0.1\n", "26A434\n"}[n]
		if e != nil || out.Truncated || out.StdoutTruncated || out.StderrTruncated || !out.StderrComplete || out.Stderr != "" || out.Stdout != expected {
			return ErrRefused
		}
	}
	return nil
}

func sameSudo(a, b os.FileInfo) bool {
	x, xok := a.Sys().(*syscall.Stat_t)
	y, yok := b.Sys().(*syscall.Stat_t)
	return xok && yok && x.Dev == y.Dev && x.Ino == y.Ino && x.Uid == y.Uid && x.Gid == y.Gid && x.Nlink == y.Nlink && a.Mode() == b.Mode() && a.Size() == b.Size() && a.ModTime().Equal(b.ModTime())
}

// CheckPlatform exposes only the already fixed read-only OS qualification.
func CheckPlatform() error {
	return sudoPlatform(execx.OSRunner{MaxOutputBytes: 512, StrictStderr: true}, runtime.GOOS, runtime.GOARCH)
}
