//go:build n1diagnostic && !n1candidate

package tart

import (
	"context"
	"errors"
	"github.com/weshofmann/boxwarden/internal/backend"
	"github.com/weshofmann/boxwarden/internal/networkdiag"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

type diagnosticChildFixture struct{ waits, stops int }

func (h *diagnosticChildFixture) Stop(context.Context) error { h.stops++; return nil }
func (h *diagnosticChildFixture) Wait(context.Context) error { h.waits++; return nil }
func (h *diagnosticChildFixture) RetainedChildLive() bool    { return h.waits == 0 }
func diagnosticBindingFixture() DiagnosticLaunchBinding {
	return DiagnosticLaunchBinding{"00112233-4455-4677-8899-aabbccddeeff", "11111111-2222-4333-8444-555555555555", [6]uint8{2, 0, 0, 0, 0, 2}}
}
func TestDiagnosticLauncherRetainsSpawnedHandleOnHelloFailure(t *testing.T) {
	b := diagnosticBindingFixture()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	generation := filepath.Join(root, b.Generation)
	os.Mkdir(generation, 0700)
	child := &diagnosticChildFixture{}
	spawned := false
	l := NewDiagnosticLauncher(validLaunchConfig(), b)
	l.spawn = func(_ context.Context, s processSpec, out *os.File) (backend.Handle, error) {
		spawned = true
		want := "--net-softnet-block=@boxwarden-n1-diagnostic:" + b.Generation + ":" + b.Nonce
		if len(s.args) != 8 || s.args[2] != want {
			t.Fatalf("selector count/bytes=%q", s.args)
		}
		for _, a := range s.args {
			if strings.Contains(a, "--net-softnet-allow") {
				t.Fatal("allow flag")
			}
		}
		raw, _ := networkdiag.Frame(networkdiag.Hello{Version: 1, Kind: "HELLO", Generation: b.Generation, Nonce: b.Nonce, CandidateMAC: [6]uint8{2, 0, 0, 0, 0, 9}, Gateway: [4]uint8{192, 168, 64, 1}})
		out.Write(raw)
		return child, nil
	}
	h, err := l.Start(t.Context(), backend.StartRequest{ObjectID: "synthetic", SerialDevice: "/dev/ttys004", GenerationDirectory: generation})
	if !spawned || h == nil || !errors.Is(err, networkdiag.ErrMetadata) {
		t.Fatalf("spawned=%v handle=%v err=%v", spawned, h, err)
	}
	if err := h.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := h.Wait(t.Context()); err != nil {
		t.Fatal(err)
	}
	if child.waits != 1 || child.stops != 1 {
		t.Fatal("owned handle not stopped/reaped")
	}
	if _, err := os.Stat(filepath.Join(generation, "tart")); !os.IsNotExist(err) {
		t.Fatal("scratch not cleaned")
	}
}
func TestDiagnosticSocketpairOriginalFlags(t *testing.T) {
	a, b, err := diagnosticSocketpair()
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	defer b.Close()
	for _, f := range []*os.File{a, b} {
		flags, _, e := syscall.Syscall(syscall.SYS_FCNTL, f.Fd(), syscall.F_GETFL, 0)
		if e != 0 || flags&syscall.O_NONBLOCK == 0 || flags&syscall.O_ACCMODE != syscall.O_RDWR {
			t.Fatal("raw file lost NONBLOCK/RDWR")
		}
		flags, _, e = syscall.Syscall(syscall.SYS_FCNTL, f.Fd(), syscall.F_GETFD, 0)
		if e != 0 || flags&syscall.FD_CLOEXEC == 0 {
			t.Fatal("original lost CLOEXEC")
		}
	}
}
func TestDiagnosticCanceledWaitCannotClaimWatchCleanup(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	h := &diagnosticHandle{Handle: &diagnosticChildFixture{}}
	if err := h.Wait(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled underlying nil Wait claimed cleanup: %v", err)
	}
}
func TestDiagnosticOwnedProcessReleaseErrorRetained(t *testing.T) {
	cause := errors.New("synthetic retained process release failed")
	h := &osProcessHandle{process: &os.Process{Pid: 4242}, done: make(chan struct{}), pollWait: func(int) (int, syscall.WaitStatus, error) { return 4242, 0, nil }, releaseProcess: func() error { return cause }}
	err := h.Wait(t.Context())
	if !errors.Is(err, cause) || !errors.Is(err, ErrDiagnosticCleanupUnproven) {
		t.Fatalf("actual process release failure discharged: %v", err)
	}
}
