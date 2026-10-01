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
	PID                    int
	Birth, Unique          uint64
	Path, SHA              string
	Device, Inode          uint64
	Kind, QualificationSHA string
}
type sampler interface {
	snapshot(context.Context) ([]process, error)
}

type censusPhase uint8

const (
	cleanupConsumers censusPhase = iota
	preinstallConsumers
)

func census(ctx context.Context, s sampler, l contract.StaticLock, self process, c contract.Catalogue) error {
	return censusInPhase(ctx, s, l, self, c, cleanupConsumers)
}

// Before L/C/trial effects, U/R are forbidden even at their exact reviewed paths.
func preflightCensus(ctx context.Context, s sampler, l contract.StaticLock, self process, c contract.Catalogue) error {
	return censusInPhase(ctx, s, l, self, c, preinstallConsumers)
}
func censusInPhase(ctx context.Context, s sampler, l contract.StaticLock, self process, c contract.Catalogue, phase censusPhase) error {
	if s == nil || ctx.Err() != nil || (phase != cleanupConsumers && phase != preinstallConsumers) {
		return ErrRefused
	}
	a, e := s.snapshot(ctx)
	if e != nil || validateProcessesInPhase(a, l, self, c, phase) != nil {
		return ErrRefused
	}
	b, e := s.snapshot(ctx)
	if e != nil || validateProcessesInPhase(b, l, self, c, phase) != nil || len(a) != len(b) {
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
func validateProcesses(xs []process, l contract.StaticLock, self process, c contract.Catalogue) error {
	return validateProcessesInPhase(xs, l, self, c, cleanupConsumers)
}
func validateProcessesInPhase(xs []process, l contract.StaticLock, self process, c contract.Catalogue, phase censusPhase) error {
	if (phase != cleanupConsumers && phase != preinstallConsumers) || !c.Valid() || len(xs) == 0 || len(xs) > maxProcesses {
		return ErrRefused
	}
	seen := map[int]bool{}
	forbidden := map[string]bool{contract.SoftnetSHA: true, "05b65d5c14e8b41e8e44b6d9fd1278de4bedbc8b735d9b99f3c748f76f75862d": true, "ab333619fc8bd7277837545e49a771baa994c01c3e8c14904ae4cc4c1f37269e": true}
	for _, i := range []int{0, 1, 5} {
		if l.Artifacts[i].SHA != "" {
			forbidden[l.Artifacts[i].SHA] = true
		}
	}
	matchedSelf := false
	for _, p := range xs {
		if p.PID <= 0 || seen[p.PID] || p.Birth == 0 || p.Unique == 0 || p.Device == 0 || p.Inode == 0 || !filepath.IsAbs(p.Path) || filepath.Clean(p.Path) != p.Path || len(p.Path) > 4096 || forbidden[p.SHA] {
			return ErrRefused
		}
		seen[p.PID] = true
		if p == self && p.Kind == "digest" && p.QualificationSHA == "" && p.SHA == l.Artifacts[4].SHA && p.Path == contract.ArtifactPath(4) {
			matchedSelf = true
			continue
		}
		actor := false
		for _, i := range []int{2, 3} {
			if p.Kind == "digest" && p.QualificationSHA == "" && p.Path == contract.ArtifactPath(i) && p.SHA == l.Artifacts[i].SHA && len(p.SHA) == 64 {
				actor = true
			}
		}
		if actor {
			if phase != cleanupConsumers {
				return ErrRefused
			}
			continue
		}
		q, key, ok := c.Lookup(p.Path)
		if !ok || p.Kind != q.Kind || p.SHA != q.SHA || p.QualificationSHA != key || p.Device != q.Leaf.Device || p.Inode != q.Leaf.Inode {
			return ErrRefused
		}
	}
	if !matchedSelf {
		return ErrRefused
	}
	return nil
}
