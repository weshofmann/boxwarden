package closeout

import (
	"os"
	"path/filepath"
	"syscall"
)

func checkedParent(path string, uid int, acl aclInspector) (os.FileInfo, error) {
	p, e := filepath.EvalSymlinks(path)
	f, se := os.Lstat(path)
	if e != nil || se != nil || p != path || f.Mode()&os.ModeSymlink != 0 || !f.IsDir() || f.Mode().Perm()&0022 != 0 || f.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 {
		return nil, ErrRefused
	}
	s, ok := f.Sys().(*syscall.Stat_t)
	if !ok || (s.Uid != 0 && int(s.Uid) != uid) || (scope{acl: acl}).aclCheck(path, f) != nil {
		return nil, ErrRefused
	}
	after, e := os.Lstat(path)
	if e != nil || !sameFile(f, after) {
		return nil, ErrRefused
	}
	return after, nil
}
