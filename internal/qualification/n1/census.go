package n1

import (
	"context"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/contract"
	"path/filepath"
	"sort"
)

const maxProcesses = 8192

// Executable pathname/file identity and birth/unique identity are bounded
// observations. They do not prove an immutable running image or atomic absence.
type process struct {
	PID           int
	Birth, Unique uint64
	Path, SHA     string
	Device, Inode uint64
}
type sampler interface {
	snapshot(context.Context) ([]process, error)
}

func census(ctx context.Context, s sampler, l contract.StaticLock, self process) error {
	if s == nil || ctx.Err() != nil {
		return ErrRefused
	}
	a, e := s.snapshot(ctx)
	if e != nil || validateProcesses(a, l, self) != nil {
		return ErrRefused
	}
	b, e := s.snapshot(ctx)
	if e != nil || validateProcesses(b, l, self) != nil || len(a) != len(b) {
		return ErrRefused
	}
	sort.Slice(a, func(i, j int) bool { return a[i].PID < a[j].PID })
	sort.Slice(b, func(i, j int) bool { return b[i].PID < b[j].PID })
	for i := range a {
		if a[i] != b[i] {
			return ErrRefused
		}
	}
	if ctx.Err() != nil {
		return ErrRefused
	}
	return nil
}
func validateProcesses(xs []process, l contract.StaticLock, self process) error {
	if len(xs) == 0 || len(xs) > maxProcesses {
		return ErrRefused
	}
	seen := map[int]bool{}
	forbidden := map[string]bool{contract.SoftnetSHA: true, "05b65d5c14e8b41e8e44b6d9fd1278de4bedbc8b735d9b99f3c748f76f75862d": true, "ab333619fc8bd7277837545e49a771baa994c01c3e8c14904ae4cc4c1f37269e": true}
	for _, i := range []int{0, 1, 5} {
		if l.Artifacts[i].SHA != "" {
			forbidden[l.Artifacts[i].SHA] = true
		}
	}
	allowed := map[string]bool{}
	for _, i := range []int{2, 3} {
		if l.Artifacts[i].SHA != "" {
			allowed[l.Artifacts[i].SHA] = true
		}
	}
	for _, image := range l.SystemImages {
		allowed[image.SHA] = true
	}
	matchedSelf := false
	for _, p := range xs {
		if p.PID <= 0 || seen[p.PID] || p.Birth == 0 || p.Unique == 0 || p.Device == 0 || p.Inode == 0 || !filepath.IsAbs(p.Path) || filepath.Clean(p.Path) != p.Path || len(p.Path) > 4096 || len(p.SHA) != 64 || forbidden[p.SHA] {
			return ErrRefused
		}
		seen[p.PID] = true
		if p == self && p.SHA == l.Artifacts[4].SHA {
			matchedSelf = true
			continue
		}
		if !allowed[p.SHA] {
			return ErrRefused
		}
	}
	if !matchedSelf {
		return ErrRefused
	}
	return nil
}
