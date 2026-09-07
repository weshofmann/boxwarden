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
	if err := writeBoundGenerationLock(filepath.Join(runtime, lockName), request); err != nil {
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

func TestExactControllerLaunchesSameGenerationFromUnheldBoundFoundation(t *testing.T) {
	runtime := privateRuntime(t)
	request := LaunchRequest{Binding: testBinding(), RuntimeDirectory: runtime, HostConfigPath: "/private/config", SessionRecordName: "dev", Host: testHostExpectation(), CA: testCAExpectation()}
	if err := writeLaunchRequest(filepath.Join(runtime, requestName), request); err != nil {
		t.Fatal(err)
	}
	if err := writeBoundGenerationLock(filepath.Join(runtime, lockName), request); err != nil {
		t.Fatal(err)
	}
	launcher := &exactLauncherFake{}
	ready := Snapshot{Binding: request.Binding, BackendRunning: true, BrokerHealthy: true, ScreenHealthy: true, PinPresent: true, CertificateCurrent: true, ProbeOK: true, ZoneMatches: true, ObservedAt: time.Now().UTC()}
	got, err := newExactController(launcher, &exactControllerFake{snapshot: ready}, startupPolicy{timeout: time.Second, interval: time.Millisecond}).StartExact(context.Background(), request)
	if err != nil || !launcher.called || launcher.request.Binding != request.Binding || got != ready {
		t.Fatalf("unheld foundation start=%#v launch=%#v err=%v", got, launcher.request, err)
	}
}

// Production break: a detached child commonly needs several authenticated
// snapshots to construct its runtime. Treating the first unavailable or
// non-ready snapshot as terminal tears down a valid claimed generation.
func TestAwaitAuthenticatedPollsUntilExactReady(t *testing.T) {
	binding := testBinding()
	ready := Snapshot{Binding: binding, BackendRunning: true, BrokerHealthy: true, ScreenHealthy: true, PinPresent: true, CertificateCurrent: true, ProbeOK: true, ZoneMatches: true, ObservedAt: time.Now().UTC()}
	calls := 0
	err := awaitAuthenticatedWithPolicy(context.Background(), binding, startupPolicy{timeout: time.Second, interval: time.Millisecond}, func(context.Context) (Snapshot, error) {
		calls++
		switch calls {
		case 1:
			return Snapshot{}, fmt.Errorf("socket not ready")
		case 2:
			return Snapshot{Binding: binding, ObservedAt: time.Now().UTC()}, nil
		default:
			return ready, nil
		}
	})
	if err != nil || calls != 3 {
		t.Fatalf("awaitAuthenticatedWithPolicy() calls=%d err=%v, want three polls and success", calls, err)
	}
}

// Production break: an authenticated snapshot cannot make an arbitrary
// generation.lock safe. Exact controller admission must verify its binding to
// the immutable request before classifying a namespace as live.
func TestExactControllerRejectsForeignGenerationLock(t *testing.T) {
	runtime := privateRuntime(t)
	request := LaunchRequest{Binding: testBinding(), RuntimeDirectory: runtime, HostConfigPath: "/private/config", SessionRecordName: "dev", Host: testHostExpectation(), CA: testCAExpectation()}
	if err := writeLaunchRequest(filepath.Join(runtime, requestName), request); err != nil {
		t.Fatal(err)
	}
	foreign := request
	foreign.Binding.BackendObject = "object-other"
	if err := writeBoundGenerationLock(filepath.Join(runtime, lockName), foreign); err != nil {
		t.Fatal(err)
	}
	controller := &exactControllerFake{snapshot: Snapshot{Binding: request.Binding, BackendRunning: true, BrokerHealthy: true, ScreenHealthy: true, PinPresent: true, CertificateCurrent: true, ProbeOK: true, ZoneMatches: true, ObservedAt: time.Now().UTC()}}
	if _, err := NewExactController(&exactLauncherFake{}, controller).StartExact(context.Background(), request); err == nil {
		t.Fatal("StartExact() accepted a generation lock bound to another request")
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

// Production break: broad root artifact admission would let a hostile or
// stale child hide arbitrary files behind an authenticated generation.
func TestValidateLiveOuterEntryFiniteCredentialAllowlist(t *testing.T) {
	credentials := []struct {
		name string
		mode os.FileMode
	}{{"client", 0o600}, {"client.pub", 0o644}, {"client-cert.pub", 0o644}, {"known_hosts", 0o600}}
	tests := []struct {
		name    string
		entry   string
		mode    os.FileMode
		dir     bool
		symlink bool
		ok      bool
	}{}
	for _, credential := range credentials {
		tests = append(tests,
			struct {
				name, entry      string
				mode             os.FileMode
				dir, symlink, ok bool
			}{"accept " + credential.name, credential.name, credential.mode, false, false, true},
			struct {
				name, entry      string
				mode             os.FileMode
				dir, symlink, ok bool
			}{"wrong mode " + credential.name, credential.name, credential.mode ^ 0o044, false, false, false},
			struct {
				name, entry      string
				mode             os.FileMode
				dir, symlink, ok bool
			}{"directory " + credential.name, credential.name, 0o700, true, false, false},
			struct {
				name, entry      string
				mode             os.FileMode
				dir, symlink, ok bool
			}{"symlink " + credential.name, credential.name, credential.mode, false, true, false})
	}
	tests = append(tests,
		struct {
			name, entry      string
			mode             os.FileMode
			dir, symlink, ok bool
		}{"serial directory", "serial", 0o700, true, false, true},
		struct {
			name, entry      string
			mode             os.FileMode
			dir, symlink, ok bool
		}{"tart serial root", "tart-serial", 0o600, false, false, false},
		struct {
			name, entry      string
			mode             os.FileMode
			dir, symlink, ok bool
		}{"operator console root", "operator-console", 0o600, false, false, false},
		struct {
			name, entry      string
			mode             os.FileMode
			dir, symlink, ok bool
		}{"arbitrary root artifact", "client.tmp", 0o600, false, false, false})
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runtime := privateRuntime(t)
			path := filepath.Join(runtime, test.entry)
			var err error
			if test.symlink {
				err = os.Symlink("target", path)
			} else if test.dir {
				err = os.Mkdir(path, test.mode)
			} else {
				err = os.WriteFile(path, []byte("test"), test.mode)
			}
			if err != nil {
				t.Fatal(err)
			}
			entries, err := os.ReadDir(runtime)
			if err != nil || len(entries) != 1 {
				t.Fatalf("ReadDir() = %#v, %v", entries, err)
			}
			err = validateLiveOuterEntry(runtime, entries[0])
			if (err == nil) != test.ok {
				t.Fatalf("validateLiveOuterEntry(%q, %#o, dir=%t) error = %v, want accepted=%t", test.entry, test.mode, test.dir, err, test.ok)
			}
		})
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
