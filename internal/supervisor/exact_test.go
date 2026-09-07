package supervisor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Production break: returning before an authenticated exact binding snapshot
// would let session persist READY based merely on child-launch success.
func TestExactControllerReturnsOnlyAuthenticatedExactSnapshot(t *testing.T) {
	binding := testBinding()
	runtime := filepath.Join(t.TempDir(), "generation")
	if err := os.Mkdir(runtime, 0o700); err != nil {
		t.Fatal(err)
	}
	launcher := &exactLauncherFake{}
	controller := &exactControllerFake{snapshot: Snapshot{Binding: binding, BackendRunning: true, BrokerHealthy: true, ScreenHealthy: true, PinPresent: true, CertificateCurrent: true, ProbeOK: true, ZoneMatches: true, ObservedAt: time.Now().UTC()}}
	got, err := NewExactController(launcher, controller).StartExact(context.Background(), LaunchRequest{Binding: binding, RuntimeDirectory: runtime, HostConfigPath: "/private/config", SessionRecordName: "dev"})
	if err != nil {
		t.Fatalf("StartExact() error = %v", err)
	}
	if got != controller.snapshot || launcher.request.Binding != binding || controller.binding != binding {
		t.Fatalf("StartExact() effects = launch %#v snapshot %#v binding %#v, want exact binding", launcher.request, got, controller.binding)
	}
	controller.snapshot.Binding = Binding{Domain: "other"}
	runtime2 := filepath.Join(t.TempDir(), "generation")
	if err := os.Mkdir(runtime2, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := NewExactController(&exactLauncherFake{}, controller).StartExact(context.Background(), LaunchRequest{Binding: binding, RuntimeDirectory: runtime2, HostConfigPath: "/private/config", SessionRecordName: "dev"}); err == nil {
		t.Fatal("StartExact() error = nil, want wrong authenticated binding rejection")
	}
}

type exactLauncherFake struct{ request LaunchRequest }

func (f *exactLauncherFake) Launch(_ context.Context, request LaunchRequest) error {
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
