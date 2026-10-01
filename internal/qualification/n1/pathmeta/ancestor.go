//go:build (darwin || linux) && (n1diagnostic || n1clipboarddiagnostic) && !n1candidate

package pathmeta

import (
	"errors"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/contract"
	"os"
	"path/filepath"
	"syscall"
)

type ancestorInspector interface{ HasExactDenyDeleteACL(string) (bool, error) }
type ancestorChecks struct {
	stat      func(string) (os.FileInfo, error)
	canonical func(string) (string, error)
	acl       func(string) (bool, error)
	platform  func() error
}

// Qualification reader ancestors only. Artifact leaves and Check remain strict.
func CheckQualificationAncestor(p string, expected os.FileInfo, i Inspector, platform func() error) error {
	if contract.ProtectedAncestorMode(p) == 0 {
		return Check(p, expected, i)
	}
	a, ok := i.(ancestorInspector)
	if !ok {
		return errors.New("qualification ancestry refused")
	}
	return checkQualificationAncestor(p, expected, ancestorChecks{os.Lstat, filepath.EvalSymlinks, a.HasExactDenyDeleteACL, platform})
}
func checkQualificationAncestor(p string, expected os.FileInfo, i ancestorChecks) error {
	mode := contract.ProtectedAncestorMode(p)
	if mode == 0 || expected == nil || i.stat == nil || i.canonical == nil || i.acl == nil || i.platform == nil {
		return errors.New("qualification ancestry refused")
	}
	admit := func(f os.FileInfo) bool {
		if f == nil || f.Mode() != os.ModeDir|os.FileMode(mode) {
			return false
		}
		s, ok := f.Sys().(*syscall.Stat_t)
		return ok && s.Uid == 501 && s.Gid == 20 && s.Nlink > 0
	}
	if !admit(expected) {
		return errors.New("qualification ancestry refused")
	}
	before, e := i.stat(p)
	canonical, ce := i.canonical(p)
	if e != nil || ce != nil || canonical != p || !admit(before) || !sameAncestor(expected, before) {
		return errors.New("qualification ancestry drift")
	}
	pe := i.platform()
	has, ae := i.acl(p)
	after, se := i.stat(p)
	canonical, ce = i.canonical(p)
	hasFinal, finalACL := i.acl(p)
	final, finalStat := i.stat(p)
	if pe != nil || ae != nil || !has || se != nil || ce != nil || canonical != p || !admit(after) || !sameAncestor(before, after) || finalACL != nil || !hasFinal || finalStat != nil || !admit(final) || !sameAncestor(after, final) {
		return errors.New("qualification ancestry drift")
	}
	return nil
}
func sameAncestor(a, b os.FileInfo) bool {
	x, xok := a.Sys().(*syscall.Stat_t)
	y, yok := b.Sys().(*syscall.Stat_t)
	return xok && yok && x.Dev == y.Dev && x.Ino == y.Ino && sameSecurityMetadata(a, b) && a.Size() == b.Size() && a.ModTime().Equal(b.ModTime()) && ancestorTimes(a, b)
}
