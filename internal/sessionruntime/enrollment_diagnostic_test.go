//go:build n1diagnostic && !n1candidate

package sessionruntime

import (
	"context"
	"errors"
	"github.com/weshofmann/boxwarden/internal/config"
	"github.com/weshofmann/boxwarden/internal/hostx"
	"github.com/weshofmann/boxwarden/internal/session"
	"github.com/weshofmann/boxwarden/internal/supervisor"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func checkDiagnosticStarterForTest(t *testing.T, s *session.Service, err error) bool {
	t.Helper()
	if s != nil || !errors.Is(err, config.ErrN1Enrollment) {
		t.Fatalf("ordinary fixture escaped exact diagnostic enrollment: %v %v", s, err)
	}
	return true
}
func diagnosticEnrollmentEnforcedForTest() bool { return true }
func configureDiagnosticProcessOwnerForTest(o *Owner, r supervisor.LaunchRequest) {
	o.deps.diagnostic.launch = nil
	o.deps.diagnostic.acquire = func(context.Context, config.Config, config.Domain, supervisor.LaunchRequest, hostx.RuntimeExpectation) (session.LaunchGuard, error) {
		fd, err := syscall.Open(r.HostConfigPath+".synthetic-launch.lock", syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
		if err != nil {
			return nil, err
		}
		f := os.NewFile(uintptr(fd), "synthetic independent Owner SH")
		if err := syscall.Flock(fd, syscall.LOCK_SH|syscall.LOCK_NB); err != nil {
			return nil, errors.Join(err, f.Close())
		}
		return &ownerGuardFixture{file: f}, nil
	}
}
func TestDiagnosticDetachedHandoffRetainsIndependentOwnerSH(t *testing.T) {
	f := newFixture(t)
	p := f.request.HostConfigPath + ".synthetic-launch.lock"
	if err := os.WriteFile(p, nil, 0600); err != nil {
		t.Fatal(err)
	}
	parent, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer parent.Close()
	if err := syscall.Flock(int(parent.Fd()), syscall.LOCK_SH|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	ex, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer ex.Close()
	launcher, err := supervisor.NewDetachedLauncher()
	if err != nil {
		t.Fatal(err)
	}
	controller, err := supervisor.NewExactController(filepath.Join(f.root, "runtime"), launcher)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	t.Cleanup(func() { controller.Stop(context.Background(), f.request.Binding) })
	snapshot, err := controller.StartExact(ctx, f.request)
	if err != nil || !snapshot.ProbeOK {
		t.Fatalf("actual synthetic diagnostic handoff: %+v %v", snapshot, err)
	}
	assertEX(t, ex, false)
	if err := parent.Close(); err != nil {
		t.Fatal(err)
	}
	assertEX(t, ex, false)
	if err := controller.Stop(ctx, f.request.Binding); err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(f.request.HostConfigPath + ".reaped"); err == nil {
			break
		}
		if raw, err := os.ReadFile(f.request.HostConfigPath + ".reaped-error"); err == nil {
			t.Fatalf("actual cleanup=%s", raw)
		}
		select {
		case <-ctx.Done():
			t.Fatal("detached cleanup deadline")
		case <-time.After(10 * time.Millisecond):
		}
	}
	assertEX(t, ex, true)
}
