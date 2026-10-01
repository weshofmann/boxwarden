package closeout

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"syscall"
)

func emptyDoctorState(ctx context.Context, p string, uid int, acl aclInspector) (err error) {
	if ctx.Err() != nil || acl == nil {
		return ErrRefused
	}
	canonical, e := filepath.EvalSymlinks(p)
	before, se := os.Lstat(p)
	if e != nil || se != nil || canonical != p || before.Mode() != os.ModeDir|0700 {
		return ErrRefused
	}
	st, ok := before.Sys().(*syscall.Stat_t)
	if !ok || int(st.Uid) != uid || (scope{acl: acl}).aclCheck(p, before) != nil {
		return ErrRefused
	}
	root, e := os.OpenRoot(p)
	if e != nil {
		return ErrRefused
	}
	defer func() { err = errors.Join(err, root.Close()) }()
	pinned, e := root.Stat(".")
	if e != nil || !sameFile(before, pinned) {
		return ErrRefused
	}
	names, e := directoryMembers(root)
	if e != nil || len(names) != 0 {
		return ErrRefused
	}
	after, e := root.Stat(".")
	visible, se := os.Lstat(p)
	if e != nil || se != nil || !sameFile(before, after) || !sameFile(before, visible) || (scope{acl: acl}).aclCheck(p, visible) != nil || ctx.Err() != nil {
		return ErrRefused
	}
	return nil
}
