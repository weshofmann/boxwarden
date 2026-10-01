package closeout

import (
	"context"
	"errors"
	"os"
	"syscall"
)

const candidateVersionParent = "/Library/Boxwarden/toolchains/softnet/0.19.0-boxwarden-n1-diagnostic.2"
const candidateTree = candidateVersionParent + "/1bb12bec8821835ada8c036426c2c03061be6cebdf4a1b658249d268fc8bde05"

var candidateParents = [...]string{"/", "/Library", "/Library/Boxwarden", "/Library/Boxwarden/toolchains", "/Library/Boxwarden/toolchains/softnet", candidateVersionParent}

// Absence of the exact digest directory is useful only with every protected
// ancestor present, canonical and unchanged throughout this observation.
func candidateAbsent(ctx context.Context, i protectedInspector) error {
	if ctx.Err() != nil || i.stat == nil || i.canonical == nil || i.acl == nil || i.open == nil {
		return ErrRefused
	}
	before := make([]os.FileInfo, len(candidateParents))
	inspect := func(p string) (os.FileInfo, error) {
		f, e := i.stat(p)
		canonical, ce := i.canonical(p)
		if e != nil || ce != nil || canonical != p || f == nil || f.Mode() != os.ModeDir|0755 {
			return nil, ErrRefused
		}
		st, ok := f.Sys().(*syscall.Stat_t)
		if !ok || st.Uid != 0 || st.Gid != 0 || st.Nlink < 1 || uint32(st.Mode)&07777 != 0755 || i.acl(p, f, "none") != nil {
			return nil, ErrRefused
		}
		return f, nil
	}
	for n, p := range candidateParents {
		f, e := inspect(p)
		if e != nil {
			return ErrRefused
		}
		before[n] = f
		opened, e := i.open(p, true)
		if e != nil || opened == nil {
			return ErrRefused
		}
		info, se := opened.Stat()
		ce := opened.Close()
		if se != nil || ce != nil || !sameFile(f, info) {
			return errors.Join(ErrRefused, se, ce)
		}
	}
	if _, e := i.stat(candidateTree); !os.IsNotExist(e) {
		return ErrRefused
	}
	for n, p := range candidateParents {
		f, e := inspect(p)
		if e != nil || !sameFile(f, before[n]) {
			return ErrRefused
		}
	}
	if _, e := i.stat(candidateTree); !os.IsNotExist(e) || ctx.Err() != nil {
		return ErrRefused
	}
	return nil
}
