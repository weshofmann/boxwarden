package closeout

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Retire never resumes a partial retirement. The only plan is the already
// observed exhaustive inventory; no recursive removal or discovery follows it.
func (g *inventoryGuard) Retire(ctx context.Context) error {
	if g == nil || g.closed || g.Revalidate(ctx) != nil {
		return ErrRefused
	}
	names := make([]string, 0, len(g.snapshots))
	for n := range g.snapshots {
		if n != "." {
			names = append(names, n)
		}
	}
	sort.Slice(names, func(i, j int) bool {
		a, b := g.snapshots[names[i]], g.snapshots[names[j]]
		if a.IsDir() != b.IsDir() {
			return !a.IsDir()
		}
		if a.IsDir() && strings.Count(names[i], "/") != strings.Count(names[j], "/") {
			return strings.Count(names[i], "/") > strings.Count(names[j], "/")
		}
		return names[i] < names[j]
	})
	remaining := map[string]bool{}
	for n := range g.snapshots {
		remaining[n] = true
	}
	for _, n := range names {
		if ctx.Err() != nil || g.check() != nil {
			return ErrRefused
		}
		parent := filepath.Dir(n)
		if parent == "." {
			parent = "."
		}
		if g.checkCurrentDirectory(parent, remaining) != nil {
			return ErrRefused
		}
		visible, e := g.root.Lstat(n)
		expected := g.snapshots[n]
		if e != nil || (!expected.IsDir() && !sameFile(visible, expected)) || (expected.IsDir() && !stableDirectory(visible, expected)) || g.scope.aclCheck(filepath.Join(g.scope.root, n), visible) != nil {
			return ErrRefused
		}
		final, e := g.root.Lstat(n)
		if e != nil || !sameFile(visible, final) || ctx.Err() != nil || g.check() != nil {
			return ErrRefused
		}
		if e = g.remove(n); e != nil {
			return errors.Join(ErrRefused, e)
		}
		if _, e = g.root.Lstat(n); !os.IsNotExist(e) {
			return ErrRefused
		}
		delete(remaining, n)
		if e = g.syncDirectory(parent); e != nil {
			return e
		}
	}
	if ctx.Err() != nil || g.check() != nil || g.checkCurrentDirectory(".", remaining) != nil {
		return ErrRefused
	}
	parentPath := filepath.Dir(g.scope.root)
	if g.scope.volume != nil && g.scope.volume() != nil {
		return ErrRefused
	}
	parentInfo, pe := checkedParent(parentPath, g.scope.uid, g.scope.acl)
	if pe != nil || !sameFile(parentInfo, g.parent) {
		return ErrRefused
	}
	parent, e := os.OpenRoot(parentPath)
	if e != nil {
		return ErrRefused
	}
	work := func() error {
		opened, e := parent.Stat(".")
		if e != nil || !sameFile(opened, parentInfo) {
			return ErrRefused
		}
		visible, e := os.Lstat(g.scope.root)
		pinned, pe := g.root.Stat(".")
		if e != nil || pe != nil || !sameFile(visible, pinned) {
			return ErrRefused
		}
		current, pe := checkedParent(parentPath, g.scope.uid, g.scope.acl)
		if pe != nil || !sameFile(current, parentInfo) || ctx.Err() != nil || g.check() != nil {
			return ErrRefused
		}
		// Parent/volume admission can take time. Re-prove the original root's
		// security and exhaustive empty membership after those observations.
		if g.checkCurrentDirectory(".", remaining) != nil {
			return ErrRefused
		}
		visible, e = os.Lstat(g.scope.root)
		pinned, pe = g.root.Stat(".")
		lastParent, lp := parent.Stat(".")
		visibleParent, vp := os.Lstat(parentPath)
		if e != nil || pe != nil || lp != nil || vp != nil || !sameFile(visible, pinned) || !stableDirectory(visible, g.snapshots["."]) || !sameFile(current, lastParent) || !sameFile(current, visibleParent) || ctx.Err() != nil || g.check() != nil {
			return ErrRefused
		}
		if e = parent.Remove(filepath.Base(g.scope.root)); e != nil {
			return errors.Join(ErrRefused, e)
		}
		if _, e = os.Lstat(g.scope.root); !os.IsNotExist(e) {
			return ErrRefused
		}
		f, e := parent.Open(".")
		if e != nil {
			return ErrRefused
		}
		se, ce := f.Sync(), f.Close()
		if se != nil || ce != nil {
			return errors.Join(ErrRefused, se, ce)
		}
		after, pe := checkedParent(parentPath, g.scope.uid, g.scope.acl)
		opened, e = parent.Stat(".")
		if pe != nil || e != nil || !stableDirectory(parentInfo, after) || !sameFile(after, opened) || ctx.Err() != nil || g.check() != nil {
			return ErrRefused
		}
		if g.scope.volume != nil && g.scope.volume() != nil {
			return ErrRefused
		}
		return nil
	}()
	ce := parent.Close()
	return errors.Join(work, ce)
}
func (g *inventoryGuard) checkCurrentDirectory(n string, remaining map[string]bool) error {
	f, e := g.root.Open(n)
	if e != nil {
		return ErrRefused
	}
	work := func() error {
		before, e := f.Stat()
		visible, ve := g.root.Lstat(n)
		if e != nil || ve != nil || !sameFile(before, visible) || !stableDirectory(before, g.snapshots[n]) || g.scope.aclCheck(filepath.Join(g.scope.root, n), visible) != nil {
			return ErrRefused
		}
		got, e := f.Readdirnames(257)
		tail, te := f.Readdirnames(1)
		if (e != nil && e != io.EOF) || len(tail) != 0 || te != io.EOF || len(got) > 256 {
			return ErrRefused
		}
		want := []string{}
		for name := range remaining {
			if name != "." && filepath.Dir(name) == n {
				want = append(want, filepath.Base(name))
			}
		}
		sort.Strings(got)
		sort.Strings(want)
		if len(got) != len(want) {
			return ErrRefused
		}
		for i := range got {
			if got[i] != want[i] {
				return ErrRefused
			}
		}
		after, e := f.Stat()
		current, ce := g.root.Lstat(n)
		if e != nil || ce != nil || !sameFile(before, after) || !sameFile(before, current) {
			return ErrRefused
		}
		return nil
	}()
	return errors.Join(work, f.Close())
}
func (g *inventoryGuard) syncDirectory(n string) error {
	f, e := g.root.Open(n)
	if e != nil {
		return ErrRefused
	}
	return errors.Join(f.Sync(), f.Close())
}
func (g *inventoryGuard) remove(n string) error {
	if g.scope.remove != nil {
		return g.scope.remove(g.root, n)
	}
	return g.root.Remove(n)
}
func (g *inventoryGuard) check() error {
	if g.scope.check != nil {
		return g.scope.check()
	}
	return nil
}
