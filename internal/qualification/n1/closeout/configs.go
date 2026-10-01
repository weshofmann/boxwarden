package closeout

import (
	"context"
	"errors"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/contract"
	"os"
	"path/filepath"
	"syscall"
)

var configNames = [5]string{"stock.source.json", "candidate.source.json", "postcloseout-doctor.json", "stock.enrolled.json", "candidate.enrolled.json"}

func retireEnrolled(ctx context.Context, path string, uid int, acl aclInspector, hashes [5]string, tick func() error, remove func(*os.Root, string) error) (err error) {
	if ctx.Err() != nil || tick == nil || remove == nil || acl == nil || tick() != nil {
		return ErrRefused
	}
	canonical, e := filepath.EvalSymlinks(path)
	info, se := os.Lstat(path)
	if e != nil || se != nil || canonical != path || info.Mode() != os.ModeDir|0700 {
		return ErrRefused
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(st.Uid) != uid {
		return ErrRefused
	}
	root, e := os.OpenRoot(path)
	if e != nil {
		return ErrRefused
	}
	g := &inventoryGuard{root: root, scope: scope{root: path, uid: uid, acl: acl, check: tick, remove: remove}, snapshots: map[string]os.FileInfo{".": info}}
	defer func() { err = errors.Join(err, g.Close()) }()
	pinned, e := root.Stat(".")
	if e != nil || !sameFile(info, pinned) || g.scope.aclCheck(path, info) != nil {
		return ErrRefused
	}
	remaining := map[string]bool{".": true}
	for i, n := range configNames {
		before, e := root.Lstat(n)
		if e != nil {
			return ErrRefused
		}
		raw, e := readNamed(ctx, root, path, n, uid, acl, 4096)
		after, se := root.Lstat(n)
		if e != nil || se != nil || contract.SHA(raw) != hashes[i] || !sameFile(before, after) || tick() != nil {
			return ErrRefused
		}
		g.snapshots[n] = after
		remaining[n] = true
	}
	if g.checkCurrentDirectory(".", remaining) != nil {
		return ErrRefused
	}
	for _, n := range configNames[3:] {
		if ctx.Err() != nil || tick() != nil || g.checkCurrentDirectory(".", remaining) != nil {
			return ErrRefused
		}
		current, e := root.Lstat(n)
		if e != nil || !sameFile(current, g.snapshots[n]) || g.scope.aclCheck(filepath.Join(path, n), current) != nil {
			return ErrRefused
		}
		final, e := root.Lstat(n)
		if e != nil || !sameFile(current, final) || ctx.Err() != nil || tick() != nil {
			return ErrRefused
		}
		if e = g.remove(n); e != nil {
			return errors.Join(ErrRefused, e)
		}
		if _, e = root.Lstat(n); !os.IsNotExist(e) {
			return ErrRefused
		}
		delete(remaining, n)
		if e = g.syncDirectory("."); e != nil {
			return ErrRefused
		}
	}
	if g.checkCurrentDirectory(".", remaining) != nil || ctx.Err() != nil || tick() != nil {
		return ErrRefused
	}
	for _, n := range configNames[:3] {
		current, e := root.Lstat(n)
		if e != nil || !sameFile(current, g.snapshots[n]) || g.scope.aclCheck(filepath.Join(path, n), current) != nil {
			return ErrRefused
		}
	}
	return nil
}
