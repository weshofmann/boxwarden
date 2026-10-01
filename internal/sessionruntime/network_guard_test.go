//go:build n1diagnostic && !n1candidate

package sessionruntime

import (
	"context"
	"errors"
	"github.com/weshofmann/boxwarden/internal/backend"
	"github.com/weshofmann/boxwarden/internal/backend/tart"
	"github.com/weshofmann/boxwarden/internal/config"
	"github.com/weshofmann/boxwarden/internal/hostx"
	"github.com/weshofmann/boxwarden/internal/session"
	"github.com/weshofmann/boxwarden/internal/supervisor"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
)

type ownerGuardFixture struct {
	file          *os.File
	once          sync.Once
	err           error
	revalidations int
}

func (g *ownerGuardFixture) Revalidate(ctx context.Context) error {
	g.revalidations++
	return ctx.Err()
}
func (g *ownerGuardFixture) Release() error {
	g.once.Do(func() { g.err = g.file.Close() })
	return g.err
}
func ownerGuard(t *testing.T) (*ownerGuardFixture, *os.File) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "launch.lock")
	if err := os.WriteFile(p, nil, 0600); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	ex, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_SH|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	g := &ownerGuardFixture{file: f}
	t.Cleanup(func() { g.Release(); ex.Close() })
	return g, ex
}
func assertEX(t *testing.T, f *os.File, want bool) {
	t.Helper()
	err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if (err == nil) != want {
		t.Fatalf("EX admission=%v wanted=%v", err, want)
	}
	if err == nil {
		syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	}
}
func withOwnerGuard(f *fixture, g *ownerGuardFixture) {
	f.owner.deps.diagnostic.acquire = func(context.Context, config.Config, config.Domain, supervisor.LaunchRequest, hostx.RuntimeExpectation) (session.LaunchGuard, error) {
		f.trace.add("guard")
		return g, nil
	}
}

// A production early release at Stop, canceled Wait, serial close or failed
// launch makes independent EX succeed before actual resource cleanup.
func TestDiagnosticOwnerIndependentSHThroughActualWait(t *testing.T) {
	f := newFixture(t)
	g, ex := ownerGuard(t)
	parent, parentEX := ownerGuard(t)
	defer parent.Release()
	assertEX(t, parentEX, false)
	withOwnerGuard(f, g)
	if err := f.owner.Start(t.Context(), f.request); err != nil {
		t.Fatal(err)
	}
	if g.revalidations == 0 {
		t.Fatal("Owner omitted independent guard")
	}
	assertEX(t, ex, false)
	if err := parent.Release(); err != nil {
		t.Fatal(err)
	}
	assertEX(t, ex, false)
	if err := f.owner.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
	assertEX(t, ex, false)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := f.owner.Wait(ctx); err != nil {
		t.Fatal(err)
	}
	assertEX(t, ex, true)
}
func TestDiagnosticOwnerPrechildCleanupAndUncertainResidue(t *testing.T) {
	for _, uncertain := range []bool{false, true} {
		t.Run(map[bool]string{false: "proved", true: "uncertain"}[uncertain], func(t *testing.T) {
			f := newFixture(t)
			g, ex := ownerGuard(t)
			withOwnerGuard(f, g)
			f.launchErr = errors.New("prechild fail")
			if uncertain {
				f.serial.closeErr = errors.New("close then error")
			}
			err := f.owner.Start(t.Context(), f.request)
			if err == nil {
				t.Fatal("accepted launch")
			}
			if errors.Is(err, supervisor.ErrRuntimeCleanupUnproven) != uncertain {
				t.Fatalf("cleanup classification=%v", err)
			}
			assertEX(t, ex, !uncertain)
		})
	}
}
func TestDiagnosticOwnerRetainsPostspawnCleanupUncertainty(t *testing.T) {
	f := newFixture(t)
	g, ex := ownerGuard(t)
	withOwnerGuard(f, g)
	f.owner.deps.diagnostic.launch = func(context.Context, tart.LaunchConfig, backend.StartRequest, supervisor.Binding) (backend.Handle, error) {
		return f.handle, tart.ErrDiagnosticCleanupUnproven
	}
	err := f.owner.Start(t.Context(), f.request)
	if !errors.Is(err, supervisor.ErrRuntimeCleanupUnproven) {
		t.Fatalf("lost postspawn cleanup uncertainty: %v", err)
	}
	assertEX(t, ex, false)
	if err := f.owner.Wait(t.Context()); !errors.Is(err, supervisor.ErrRuntimeCleanupUnproven) {
		t.Fatalf("nil second Wait discharged residue: %v", err)
	}
}
func TestDiagnosticOwnerCanceledWaitRetainsUntilActualReapAndMaintenanceJoin(t *testing.T) {
	f := newFixture(t)
	g, ex := ownerGuard(t)
	withOwnerGuard(f, g)
	if err := f.owner.Start(t.Context(), f.request); err != nil {
		t.Fatal(err)
	}
	maintenance := make(chan struct{})
	canceled := make(chan struct{})
	f.owner.maintenanceCancel = func() { close(canceled) }
	f.owner.maintenanceDone = maintenance
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	done := make(chan error, 1)
	go func() { done <- f.owner.Wait(ctx) }()
	<-f.handle.waiting
	assertEX(t, ex, false)
	select {
	case err := <-done:
		t.Fatalf("caller cancellation shortened actual reap: %v", err)
	default:
	}
	f.handle.Stop(t.Context())
	<-canceled
	assertEX(t, ex, false)
	close(maintenance)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	assertEX(t, ex, true)
}

type guardSerialClose struct {
	serialRuntime
	close func() error
}

func (s guardSerialClose) Close() error { return s.close() }
func TestDiagnosticOwnerSHSurvivesCredentialsAndActualSerialClose(t *testing.T) {
	f, _ := readyFixture(t)
	g, ex := ownerGuard(t)
	withOwnerGuard(f, g)
	original := f.owner.deps.serial
	closed := false
	f.owner.deps.serial = func(ctx context.Context, p string) (serialRuntime, error) {
		s, err := original(ctx, p)
		return guardSerialClose{s, func() error {
			closed = true
			assertEX(t, ex, false)
			for _, name := range []string{"client", "client-cert.pub", "known_hosts"} {
				if _, err := os.Lstat(filepath.Join(f.request.RuntimeDirectory, name)); !os.IsNotExist(err) {
					t.Fatalf("credentials not cleaned before serial teardown: %s %v", name, err)
				}
			}
			return s.Close()
		}}, err
	}
	for _, op := range []func(context.Context) error{func(ctx context.Context) error { return f.owner.Start(ctx, f.request) }, f.owner.Bootstrap, f.owner.Ready} {
		if err := op(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	assertEX(t, ex, false)
	f.owner.Stop(t.Context())
	if err := f.owner.Wait(t.Context()); err != nil {
		t.Fatal(err)
	}
	if !closed {
		t.Fatal("actual serial Close omitted")
	}
	assertEX(t, ex, true)
}

func TestDiagnosticUnclaimedDiskCloseRetainsGuardOnUncertainty(t *testing.T) {
	for _, uncertain := range []bool{false, true} {
		t.Run(map[bool]string{false: "proved", true: "uncertain"}[uncertain], func(t *testing.T) {
			g, ex := ownerGuard(t)
			o := &Owner{}
			o.diagnostic.guard = g
			original := errors.New("prechild refusal")
			var closeErr error
			if uncertain {
				closeErr = errors.New("unclaimed disk lease close failed")
			}
			result := o.finishDiagnosticPrechild(o.finishUnclaimedDiagnosticDisks(original, closeErr))
			if !errors.Is(result, original) || errors.Is(result, supervisor.ErrRuntimeCleanupUnproven) != uncertain {
				t.Fatalf("cleanup result=%v", result)
			}
			assertEX(t, ex, !uncertain)
		})
	}
}
