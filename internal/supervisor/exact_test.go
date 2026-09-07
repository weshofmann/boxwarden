package supervisor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// Production break: returning before an authenticated exact binding snapshot
// would let session persist READY based merely on child-launch success.
func TestExactControllerReturnsOnlyAuthenticatedExactSnapshot(t *testing.T) {
	binding := testBinding()
	runtime := filepath.Join(t.TempDir(), "generation")
	launcher := &exactLauncherFake{}
	controller := &exactControllerFake{snapshot: Snapshot{Binding: binding, BackendRunning: true, BrokerHealthy: true, ScreenHealthy: true, PinPresent: true, CertificateCurrent: true, ProbeOK: true, ZoneMatches: true, ObservedAt: time.Now().UTC()}}
	got, err := NewExactController(launcher, controller).StartExact(context.Background(), LaunchRequest{Binding: binding, RuntimeDirectory: runtime, HostConfigPath: "/private/config", SessionRecordName: "dev", Host: testHostExpectation(), CA: testCAExpectation()})
	if err != nil {
		t.Fatalf("StartExact() error = %v", err)
	}
	if got != controller.snapshot || launcher.request.Binding != binding || controller.binding != binding {
		t.Fatalf("StartExact() effects = launch %#v snapshot %#v binding %#v, want exact binding", launcher.request, got, controller.binding)
	}
	controller.snapshot.Binding = Binding{Domain: "other"}
	runtime2 := filepath.Join(t.TempDir(), "generation")
	if _, err := NewExactController(&exactLauncherFake{}, controller).StartExact(context.Background(), LaunchRequest{Binding: binding, RuntimeDirectory: runtime2, HostConfigPath: "/private/config", SessionRecordName: "dev", Host: testHostExpectation(), CA: testCAExpectation()}); err == nil {
		t.Fatal("StartExact() error = nil, want wrong authenticated binding rejection")
	}
}

// Production break: launching a second child when an exact generation already
// contains live artifacts races its owner and can corrupt the retained runtime.
func TestExactControllerReconcilesAuthenticatedLiveGenerationWithoutLaunch(t *testing.T) {
	binding := testBinding()
	runtime := privateRuntime(t)
	request := LaunchRequest{Binding: binding, RuntimeDirectory: runtime, HostConfigPath: "/private/config", SessionRecordName: "dev", Host: testHostExpectation(), CA: testCAExpectation()}
	if err := writeLaunchRequest(filepath.Join(runtime, requestName), request); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(runtime, lockName), []byte("child still starting"), 0o600); err != nil {
		t.Fatal(err)
	}
	lock, err := os.OpenFile(filepath.Join(runtime, lockName), os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	controller := &exactControllerFake{snapshot: Snapshot{Binding: binding, BackendRunning: true, BrokerHealthy: true, ScreenHealthy: true, PinPresent: true, CertificateCurrent: true, ProbeOK: true, ZoneMatches: true, ObservedAt: time.Now().UTC()}}
	launcher := &exactLauncherFake{}
	got, err := NewExactController(launcher, controller).StartExact(context.Background(), request)
	if err != nil {
		t.Fatalf("StartExact() error = %v", err)
	}
	if launcher.called {
		t.Fatal("StartExact() launched a second child for authenticated live generation")
	}
	if got != controller.snapshot || controller.binding != binding {
		t.Fatalf("StartExact() snapshot/binding = %#v/%#v, want authenticated exact live owner", got, controller.binding)
	}
}

// Production break: serial endpoints at the generation root blur serialx's
// nested ownership boundary and must not be authenticated as live state.
func TestExactControllerRejectsSerialEndpointAtGenerationRoot(t *testing.T) {
	runtime := privateRuntime(t)
	request := LaunchRequest{Binding: testBinding(), RuntimeDirectory: runtime, HostConfigPath: "/private/config", SessionRecordName: "dev", Host: testHostExpectation(), CA: testCAExpectation()}
	if err := writeLaunchRequest(filepath.Join(runtime, requestName), request); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(runtime, "tart-serial"), []byte("foreign"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewExactController(&exactLauncherFake{}, &exactControllerFake{}).StartExact(context.Background(), request); err == nil {
		t.Fatal("StartExact() accepted a serial endpoint at generation root")
	}
}

type exactLauncherFake struct {
	request LaunchRequest
	called  bool
}

func (f *exactLauncherFake) Launch(_ context.Context, request LaunchRequest) error {
	f.called = true
	f.request = request
	return nil
}

type exactControllerFake struct {
	binding  Binding
	snapshot Snapshot
}

func (f *exactControllerFake) Snapshot(_ context.Context, binding Binding) (Snapshot, error) {
	f.binding = binding
	if f.snapshot.Binding == (Binding{}) {
		return Snapshot{}, fmt.Errorf("missing snapshot")
	}
	return f.snapshot, nil
}
func (*exactControllerFake) Stop(context.Context, Binding) error { return nil }
