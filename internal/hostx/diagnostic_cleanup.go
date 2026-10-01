//go:build n1diagnostic && n1cleanup && !n1candidate && (darwin || linux)

package hostx

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/weshofmann/boxwarden/internal/execx"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/clock"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/fixed"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"syscall"
)

var ErrDiagnosticCleanup = errors.New("diagnostic cleanup refused or unknown")

// Separate from SH. Only the dedicated qualification build includes this
// capability. No descriptor, arbitrary root/digest, repair or retry is exposed.
type DiagnosticCleanupGuard struct {
	mu                sync.Mutex
	p                 RootedPublisher
	m                 Manifest
	manifest          []byte
	files             []*os.File
	paths             []string
	snapshots         [][]os.FileInfo
	digest, parent    *os.Root
	syncFile          *os.File
	dirInfo           os.FileInfo
	ancestors         []os.FileInfo
	ancestorPaths     []string
	admission         func(context.Context) error
	liveWindow        func() error
	attempted, closed bool
	closeErr          error
	fail              func(string) error
}

// Actual root admission validates operator directory identity/membership, not
// root's effective groups. Config selectors cannot alter the removal target.
func (s SystemDoctor) AcquireDiagnosticCleanup(ctx context.Context, r Request) (*DiagnosticCleanupGuard, error) {
	if os.Getuid() != 0 || os.Geteuid() != 0 {
		return nil, ErrDiagnosticCleanup
	}
	if len(r.ConfiguredStateRoots) != 1 || r.ConfiguredStateRoots[0] != "/Volumes/BoxwardenAlphaQualification/n1-diagnostic-20260930-state" || r.TartPath != "/Users/devel/Library/Application Support/boxwarden/toolchains/tart/2.32.1/tart.app/Contents/MacOS/tart" || r.TartHome != "/Users/devel/Library/Application Support/boxwarden/tart" || r.SoftnetPath != "/Users/devel/Backup/boxwarden_archive/n1-diagnostic-20260930/package/artifacts/softnet-diagnostic" {
		return nil, ErrDiagnosticCleanup
	}
	inspector := s.inspector
	if inspector == nil {
		n := NewOSDoctorInspector().(*osDoctorInspector)
		op, e := n.LookupOperator(501)
		if e != nil || op != (Operator{501, "devel", "/Users/devel"}) {
			return nil, ErrDiagnosticCleanup
		}
		n.policyOperator = "devel"
		n.runner = execx.OSRunner{MaxOutputBytes: 16 << 10, StrictStderr: true}
		inspector = n
	}
	s.inspector = inspector
	check := func(ctx context.Context) (Manifest, error) {
		a := s.inspectPolicy(ctx, r, false)
		m := a.manifest
		if a.report.Status != Healthy || m.Operator != (Operator{501, "devel", "/Users/devel"}) || m.Group.ID != 501 || m.Group.Name != OperatorGroupName || len(m.Group.Members) != 1 || m.Group.Members[0] != 501 || ctx.Err() != nil {
			return Manifest{}, ErrDiagnosticCleanup
		}
		return m, nil
	}
	m, e := check(ctx)
	if e != nil {
		return nil, e
	}
	p := RootedPublisher{Root: productionToolchainPath(), platform: inspector.Platform(), ACL: OSACLInspector{}}
	g, e := acquireDiagnosticCleanup(ctx, p, m, nil)
	if e != nil {
		return nil, e
	}
	g.admission = func(ctx context.Context) error {
		now, e := check(ctx)
		a, _ := json.Marshal(now)
		if e != nil || string(a) != string(g.manifest) {
			return ErrDiagnosticCleanup
		}
		return nil
	}
	inputs, e := fixed.ReadInputs()
	if e != nil {
		return nil, errors.Join(ErrDiagnosticCleanup, g.Close())
	}
	window := clock.New(inputs.Handoff)
	g.liveWindow = func() error {
		now, e := fixed.ReadInputs()
		r, ce := clock.Now()
		if e != nil || ce != nil || now.LockSHA != inputs.LockSHA || now.HandoffSHA != inputs.HandoffSHA || window.Check(r) != nil {
			return ErrDiagnosticCleanup
		}
		return nil
	}
	if e = g.Revalidate(ctx); e != nil {
		return nil, errors.Join(e, g.Close())
	}
	return g, nil
}
func acquireDiagnosticCleanup(ctx context.Context, p RootedPublisher, m Manifest, after func()) (*DiagnosticCleanupGuard, error) {
	g := &DiagnosticCleanupGuard{p: p, m: m}
	g.manifest, _ = json.Marshal(m)
	fail := func(e error) (*DiagnosticCleanupGuard, error) { return nil, errors.Join(e, g.Close()) }
	if e := g.validate(ctx); e != nil {
		return fail(e)
	}
	for _, name := range []string{"launch.lock", "softnet", "manifest.json"} {
		path := filepath.Join(p.finalDir(), name)
		before, e := snapshotPath(path)
		if e != nil {
			return fail(ErrDiagnosticCleanup)
		}
		digest := ""
		if name == "softnet" {
			digest = p.expectedDigest()
		}
		if name == "launch.lock" {
			digest = emptyFileSHA256
		}
		f, e := openVerifiedRegular(path, digest, false)
		if e != nil {
			return fail(ErrDiagnosticCleanup)
		}
		g.files = append(g.files, f)
		g.paths = append(g.paths, path)
		g.snapshots = append(g.snapshots, before)
	}
	if syscall.Flock(int(g.files[0].Fd()), syscall.LOCK_EX|syscall.LOCK_NB) != nil {
		return fail(ErrDiagnosticCleanup)
	}
	if after != nil {
		after()
	}
	if e := g.revalidate(ctx); e != nil {
		return fail(e)
	}
	var e error
	g.parent, e = os.OpenRoot(filepath.Dir(p.finalDir()))
	if e != nil {
		return fail(ErrDiagnosticCleanup)
	}
	g.digest, e = g.parent.OpenRoot(filepath.Base(p.finalDir()))
	if e != nil {
		return fail(ErrDiagnosticCleanup)
	}
	g.ancestors, e = snapshotPath(filepath.Dir(p.finalDir()))
	if e != nil {
		return fail(ErrDiagnosticCleanup)
	}
	for current := filepath.Dir(p.finalDir()); ; current = filepath.Dir(current) {
		g.ancestorPaths = append(g.ancestorPaths, current)
		if current == "/" {
			break
		}
	}
	for i, j := 0, len(g.ancestorPaths)-1; i < j; i, j = i+1, j-1 {
		g.ancestorPaths[i], g.ancestorPaths[j] = g.ancestorPaths[j], g.ancestorPaths[i]
	}
	g.dirInfo, e = g.digest.Stat(".")
	if e != nil {
		return fail(ErrDiagnosticCleanup)
	}
	g.syncFile, e = g.parent.Open(".")
	if e != nil {
		return fail(ErrDiagnosticCleanup)
	}
	if e = g.checkPinned(); e != nil {
		return fail(e)
	}
	return g, nil
}
func (g *DiagnosticCleanupGuard) live(ctx context.Context) error {
	if ctx.Err() != nil || g.liveWindow != nil && g.liveWindow() != nil {
		return ErrDiagnosticCleanup
	}
	return nil
}
func (g *DiagnosticCleanupGuard) validate(ctx context.Context) error {
	if g.live(ctx) != nil {
		return ErrDiagnosticCleanup
	}
	p := g.p
	r := InstallRequest{Version: InstallRequestVersion, Tart: g.m.Tart, TartHome: g.m.TartHome}
	c := Caller{UID: g.m.Operator.UID, Name: g.m.Operator.Name, Home: g.m.Operator.Home}
	if p.validateRootParent() != nil || p.validateNoCurrentPointer() != nil || p.validatePairedInputs(r, c) != nil || p.validateCompleteTree(r, c, g.m.Group) != nil {
		return ErrDiagnosticCleanup
	}
	m, e := p.readInstalledManifest()
	a, _ := json.Marshal(m)
	if e != nil || string(a) != string(g.manifest) {
		return ErrDiagnosticCleanup
	}
	return nil
}
func (g *DiagnosticCleanupGuard) revalidate(ctx context.Context) error {
	if g.closed || g.attempted || g.validate(ctx) != nil {
		return ErrDiagnosticCleanup
	}
	for i, f := range g.files {
		now, e := snapshotPath(g.paths[i])
		info, e2 := f.Stat()
		if e != nil || e2 != nil || !sameSnapshots(g.snapshots[i], now) || !sameIdentity(info, now[len(now)-1]) || links(info) != 1 {
			return ErrDiagnosticCleanup
		}
	}
	if g.admission != nil && g.admission(ctx) != nil {
		return ErrDiagnosticCleanup
	}
	if g.digest != nil {
		return g.checkPinned()
	}
	return nil
}
func (g *DiagnosticCleanupGuard) Revalidate(ctx context.Context) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.revalidate(ctx)
}
func cleanupMetadata(a, b os.FileInfo) bool {
	if !sameIdentity(a, b) || a.Mode() != b.Mode() {
		return false
	}
	au, ag, ao := ownership(a)
	bu, bg, bo := ownership(b)
	return ao && bo && au == bu && ag == bg
}
func (g *DiagnosticCleanupGuard) checkParent() error {
	now, e := snapshotPath(filepath.Dir(g.p.finalDir()))
	if e != nil || len(now) != len(g.ancestors) {
		return ErrDiagnosticCleanup
	}
	for i, info := range now {
		acl, e := g.p.aclInspector().HasExtendedACL(g.ancestorPaths[i])
		if e != nil || acl || !cleanupMetadata(info, g.ancestors[i]) {
			return ErrDiagnosticCleanup
		}
	}
	f, e := g.syncFile.Stat()
	p, e2 := g.parent.Stat(".")
	if e != nil || e2 != nil || !cleanupMetadata(f, now[len(now)-1]) || !cleanupMetadata(p, f) {
		return ErrDiagnosticCleanup
	}
	r := InstallRequest{Version: InstallRequestVersion, Tart: g.m.Tart, TartHome: g.m.TartHome}
	c := Caller{UID: g.m.Operator.UID, Name: g.m.Operator.Name, Home: g.m.Operator.Home}
	if g.p.validateRootParent() != nil || g.p.validateNoCurrentPointer() != nil || g.p.validatePairedInputs(r, c) != nil {
		return ErrDiagnosticCleanup
	}
	return nil
}
func (g *DiagnosticCleanupGuard) checkPinned() error {
	if g.checkParent() != nil {
		return ErrDiagnosticCleanup
	}
	d, e := g.digest.Stat(".")
	p, e2 := g.parent.Lstat(filepath.Base(g.p.finalDir()))
	f, e3 := g.syncFile.Stat()
	pi, e4 := g.parent.Stat(".")
	if e != nil || e2 != nil || e3 != nil || e4 != nil || !sameIdentity(d, g.dirInfo) || !sameIdentity(p, g.dirInfo) || !sameIdentity(f, pi) || !cleanupMetadata(d, g.dirInfo) || !cleanupMetadata(p, g.dirInfo) || !d.IsDir() {
		return ErrDiagnosticCleanup
	}
	a, aok := d.Sys().(*syscall.Stat_t)
	b, bok := pi.Sys().(*syscall.Stat_t)
	if !aok || !bok || a.Dev != b.Dev {
		return ErrDiagnosticCleanup
	}
	acl, e := g.p.aclInspector().HasExtendedACL(g.p.finalDir())
	if e != nil || acl {
		return ErrDiagnosticCleanup
	}
	return nil
}
func (g *DiagnosticCleanupGuard) checkpoint(s string) error {
	if g.fail != nil && g.fail(s) != nil {
		return ErrDiagnosticCleanup
	}
	return nil
}
func (g *DiagnosticCleanupGuard) remaining(names []string) error {
	if g.checkPinned() != nil {
		return ErrDiagnosticCleanup
	}
	f, e := g.digest.Open(".")
	if e != nil {
		return ErrDiagnosticCleanup
	}
	got, e := f.Readdirnames(4)
	ce := f.Close()
	if e != nil && e != io.EOF || ce != nil {
		return ErrDiagnosticCleanup
	}
	sort.Strings(got)
	wanted := append([]string{}, names...)
	sort.Strings(wanted)
	if len(got) != len(wanted) {
		return ErrDiagnosticCleanup
	}
	for i := range got {
		if got[i] != wanted[i] {
			return ErrDiagnosticCleanup
		}
	}
	for _, name := range names {
		idx := 0
		if name == "softnet" {
			idx = 1
		} else if name == "manifest.json" {
			idx = 2
		}
		now, e := snapshotPath(g.paths[idx])
		fi, e2 := g.files[idx].Stat()
		if e != nil || e2 != nil || !sameSnapshots(g.snapshots[idx], now) || !sameIdentity(fi, now[len(now)-1]) || links(fi) != 1 {
			return ErrDiagnosticCleanup
		}
		mode := uint32(0444)
		gid := g.p.expectedRootGID()
		if idx == 0 {
			mode = 0440
			gid = g.m.Group.ID
		} else if idx == 1 {
			mode = g.p.expectedSoftnetMode()
			gid = g.m.Group.ID
		}
		uid, gotgid, ok := ownership(fi)
		if !ok || uid != g.p.expectedRootUID() || gotgid != gid || unixMode(fi) != mode || g.p.validateFile(g.paths[idx], uid, gid, os.FileMode(mode), "") != nil {
			return ErrDiagnosticCleanup
		}
		if idx == 2 {
			f, e := g.digest.Open(name)
			if e != nil {
				return ErrDiagnosticCleanup
			}
			raw, re := io.ReadAll(io.LimitReader(f, 16385))
			ce := f.Close()
			if re != nil || ce != nil || string(raw) != string(g.manifest) {
				return ErrDiagnosticCleanup
			}
		} else {
			sha := emptyFileSHA256
			if idx == 1 {
				sha = g.p.expectedDigest()
			}
			f, e := openVerifiedRegular(g.paths[idx], sha, false)
			if e != nil {
				return ErrDiagnosticCleanup
			}
			if f.Close() != nil {
				return ErrDiagnosticCleanup
			}
		}
	}
	return nil
}

// RemoveExact consumes the guard before the first effect. A failed operation,
// including an after-effect return error, can never be retried or inferred.
func (g *DiagnosticCleanupGuard) RemoveExact(ctx context.Context) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.revalidate(ctx) != nil {
		g.attempted = true
		return ErrDiagnosticCleanup
	}
	g.attempted = true
	names := []string{"softnet", "manifest.json", "launch.lock"}
	for i, name := range names {
		if g.live(ctx) != nil || g.remaining(names[i:]) != nil || g.checkpoint("before-"+name) != nil {
			return ErrDiagnosticCleanup
		}
		if g.digest.Remove(name) != nil || g.checkpoint("after-"+name) != nil {
			return ErrDiagnosticCleanup
		}
	}
	if g.live(ctx) != nil || g.remaining(nil) != nil || g.checkpoint("before-directory") != nil {
		return ErrDiagnosticCleanup
	}
	if g.parent.Remove(filepath.Base(g.p.finalDir())) != nil || g.checkpoint("after-directory") != nil {
		return ErrDiagnosticCleanup
	}
	if g.live(ctx) != nil || g.checkParent() != nil || g.checkpoint("before-fsync") != nil || g.syncFile.Sync() != nil || g.checkpoint("after-fsync") != nil {
		return ErrDiagnosticCleanup
	}
	return nil
}
func (g *DiagnosticCleanupGuard) Close() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed {
		return g.closeErr
	}
	g.closed = true
	for _, r := range []*os.Root{g.digest, g.parent} {
		if r != nil {
			g.closeErr = errors.Join(g.closeErr, r.Close())
		}
	}
	if g.syncFile != nil {
		g.closeErr = errors.Join(g.closeErr, g.syncFile.Close())
	}
	for i := len(g.files) - 1; i >= 0; i-- {
		g.closeErr = errors.Join(g.closeErr, g.files[i].Close())
	}
	g.closeErr = errors.Join(g.closeErr, g.checkpoint("close"))
	return g.closeErr
}
