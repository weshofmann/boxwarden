//go:build n1diagnostic && !n1candidate && (darwin || linux)

package hostx

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"syscall"
)

var (
	ErrDiagnosticLaunchBusy  = errors.New("diagnostic launch lock is exclusively held")
	ErrDiagnosticLaunchDrift = errors.New("diagnostic toolchain admission drift")
	ErrDiagnosticGuardClosed = errors.New("diagnostic launch guard is closed")
)

// DiagnosticLaunchGuard owns an independently opened read-only, close-on-exec
// shared lock. It exports no descriptor or pathname. Release is explicit and
// idempotent; callers must retain it through actual resource cleanup. Process
// exit closes descriptors and cannot establish that resources were reaped.
// It is quiescence, never supervisor ownership or authorization to clean up.
type DiagnosticLaunchGuard struct {
	mu            sync.Mutex
	publisher     RootedPublisher
	expected      Manifest
	manifestBytes []byte
	files         []*os.File
	paths         []string
	snapshots     [][]os.FileInfo
	admission     func(context.Context) error
	closed        bool
	releaseErr    error
}

// AcquireDiagnosticLaunch repeats this read-only doctor's full current host
// admission before and under SH and binds it to the supplied previous facts.
// The tree is fixed by the compiled artifact identity; there is no root/path,
// digest, descriptor or enrollment override. Task3c owns exact enrollment
// admission and separate parent/Owner lifetime scopes.
func (s SystemDoctor) AcquireDiagnosticLaunch(ctx context.Context, request Request, expected RuntimeExpectation) (*DiagnosticLaunchGuard, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := expected.Manifest.Validate(); err != nil {
		return nil, fmt.Errorf("%w: expected manifest", ErrDiagnosticLaunchDrift)
	}
	if expected.SoftnetBinDir != filepath.Dir(QualifiedSoftnetPath) {
		return nil, ErrDiagnosticLaunchDrift
	}
	request.ConfiguredStateRoots = append([]string(nil), request.ConfiguredStateRoots...)
	expected.Manifest.Group.Members = append([]int(nil), expected.Manifest.Group.Members...)
	admitted, err := s.CheckRuntime(ctx, request)
	if err != nil || !sameDiagnosticExpectation(admitted, expected) {
		return nil, fmt.Errorf("%w: current host facts", ErrDiagnosticLaunchDrift)
	}
	inspector := s.inspector
	if inspector == nil {
		inspector = NewOSDoctorInspector()
	}
	p := RootedPublisher{Root: productionToolchainPath(), platform: inspector.Platform(), ACL: OSACLInspector{}}
	guard, err := acquireDiagnosticTree(ctx, p, expected.Manifest, nil)
	if err != nil {
		return nil, err
	}
	guard.admission = func(ctx context.Context) error {
		got, err := s.CheckRuntime(ctx, request)
		if err != nil || !sameDiagnosticExpectation(got, expected) {
			return fmt.Errorf("%w: repeated host facts", ErrDiagnosticLaunchDrift)
		}
		return nil
	}
	if err = guard.Revalidate(ctx); err != nil {
		return nil, errors.Join(err, guard.Release())
	}
	return guard, nil
}

func sameDiagnosticExpectation(a, b RuntimeExpectation) bool {
	x, err1 := json.Marshal(a.Manifest)
	y, err2 := json.Marshal(b.Manifest)
	return err1 == nil && err2 == nil && string(x) == string(y) && a.SoftnetBinDir == b.SoftnetBinDir
}

// Injection is unexported and used only by ordinary private synthetic trees.
func acquireDiagnosticTree(ctx context.Context, p RootedPublisher, expected Manifest, afterAcquire func()) (*DiagnosticLaunchGuard, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if SoftnetVersion != "0.19.0-boxwarden-n1-diagnostic.2" {
		return nil, ErrDiagnosticLaunchDrift
	}
	expected.Group.Members = append([]int(nil), expected.Group.Members...)
	data, err := json.Marshal(expected)
	if err != nil {
		return nil, ErrDiagnosticLaunchDrift
	}
	g := &DiagnosticLaunchGuard{publisher: p, expected: expected, manifestBytes: data}
	fail := func(err error) (*DiagnosticLaunchGuard, error) { return nil, errors.Join(err, g.Release()) }
	if err = g.validateTree(ctx); err != nil {
		return fail(err)
	}
	for _, name := range []string{"launch.lock", "softnet", "manifest.json"} {
		path := filepath.Join(p.finalDir(), name)
		before, err := snapshotPath(path)
		if err != nil {
			return fail(fmt.Errorf("%w: path snapshot", ErrDiagnosticLaunchDrift))
		}
		digest := ""
		if name == "softnet" {
			digest = p.expectedDigest()
		}
		if name == "launch.lock" {
			digest = emptyFileSHA256
		}
		f, err := openVerifiedRegular(path, digest, false)
		if err != nil {
			return fail(fmt.Errorf("%w: descriptor open", ErrDiagnosticLaunchDrift))
		}
		g.files = append(g.files, f)
		g.paths = append(g.paths, path)
		g.snapshots = append(g.snapshots, before)
		info, err := f.Stat()
		if err != nil || !sameIdentity(before[len(before)-1], info) {
			return fail(ErrDiagnosticLaunchDrift)
		}
	}
	if err = syscall.Flock(int(g.files[0].Fd()), syscall.LOCK_SH|syscall.LOCK_NB); err != nil {
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			return fail(ErrDiagnosticLaunchBusy)
		}
		return fail(fmt.Errorf("%w: shared lock unavailable", ErrDiagnosticLaunchDrift))
	}
	if afterAcquire != nil {
		afterAcquire()
	}
	if err = g.Revalidate(ctx); err != nil {
		return fail(err)
	}
	return g, nil
}

func (g *DiagnosticLaunchGuard) validateTree(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	p := g.publisher
	if err := p.validateRootParent(); err != nil {
		return fmt.Errorf("%w: root parent", ErrDiagnosticLaunchDrift)
	}
	if err := p.validateNoCurrentPointer(); err != nil {
		return fmt.Errorf("%w: mutable current pointer", ErrDiagnosticLaunchDrift)
	}
	r := InstallRequest{Version: InstallRequestVersion, Tart: g.expected.Tart, TartHome: g.expected.TartHome}
	c := Caller{UID: g.expected.Operator.UID, Name: g.expected.Operator.Name, Home: g.expected.Operator.Home}
	if err := p.validatePairedInputs(r, c); err != nil {
		return fmt.Errorf("%w: paired tool", ErrDiagnosticLaunchDrift)
	}
	if err := p.validateCompleteTree(r, c, g.expected.Group); err != nil {
		return fmt.Errorf("%w: exact tree", ErrDiagnosticLaunchDrift)
	}
	m, err := p.readInstalledManifest()
	if err != nil {
		return fmt.Errorf("%w: manifest", ErrDiagnosticLaunchDrift)
	}
	data, err := json.Marshal(m)
	if err != nil || string(data) != string(g.manifestBytes) {
		return fmt.Errorf("%w: manifest changed", ErrDiagnosticLaunchDrift)
	}
	return ctx.Err()
}

// Revalidate repeats complete exact-tree admission and compares every held
// descriptor with its original non-symlink path/ancestor snapshot under SH.
// Cancellation refuses the operation but never releases the lock.
func (g *DiagnosticLaunchGuard) Revalidate(ctx context.Context) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed {
		return ErrDiagnosticGuardClosed
	}
	if err := g.validateTree(ctx); err != nil {
		return err
	}
	for index, f := range g.files {
		now, err := snapshotPath(g.paths[index])
		if err != nil || !sameSnapshots(g.snapshots[index], now) {
			return ErrDiagnosticLaunchDrift
		}
		info, err := f.Stat()
		if err != nil || !info.Mode().IsRegular() || links(info) != 1 || !sameIdentity(now[len(now)-1], info) {
			return ErrDiagnosticLaunchDrift
		}
		uid, gid, ok := ownership(info)
		mode := os.FileMode(manifestMode)
		wantedGID := g.publisher.expectedRootGID()
		switch index {
		case 0:
			mode = 0o440
			wantedGID = g.expected.Group.ID
			if info.Size() != 0 {
				return ErrDiagnosticLaunchDrift
			}
		case 1:
			mode = os.FileMode(g.publisher.expectedSoftnetMode())
			wantedGID = g.expected.Group.ID
		}
		if !ok || uid != g.publisher.expectedRootUID() || gid != wantedGID || unixMode(info) != uint32(mode) {
			return ErrDiagnosticLaunchDrift
		}
	}
	if g.admission != nil {
		if err := g.admission(ctx); err != nil {
			return err
		}
	}
	return ctx.Err()
}

// Release closes each owned descriptor exactly once, including the SH file.
// It makes no reap/teardown claim. The first close result is retained across
// later calls; an error is not repaired or turned into success by retry.
func (g *DiagnosticLaunchGuard) Release() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed {
		return g.releaseErr
	}
	g.closed = true
	for i := len(g.files) - 1; i >= 0; i-- {
		g.releaseErr = errors.Join(g.releaseErr, g.files[i].Close())
	}
	return g.releaseErr
}
