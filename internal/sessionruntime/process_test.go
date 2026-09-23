package sessionruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weshofmann/boxwarden/internal/app"
	"github.com/weshofmann/boxwarden/internal/backend"
	"github.com/weshofmann/boxwarden/internal/backend/tart"
	"github.com/weshofmann/boxwarden/internal/config"
	"github.com/weshofmann/boxwarden/internal/hostx"
	"github.com/weshofmann/boxwarden/internal/session"
	"github.com/weshofmann/boxwarden/internal/sshx"
	"github.com/weshofmann/boxwarden/internal/supervisor"
)

// TestMain intercepts only helper argv in this test binary. The production
// detached launcher keeps its real fixed argv, closed env, Setsid, and cwd.
func TestMain(m *testing.M) {
	if len(os.Args) == 4 && os.Args[1] == "internal" && os.Args[2] == "session-supervisor" {
		processExit(runProcessOwner(os.Args[3]))
	}
	if len(os.Args) == 3 && os.Args[1] == "test-initiate" {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		err := app.Run(ctx, []string{"--config", os.Args[2], "--domain", "work", "session", "start", "dev"}, app.Options{
			Output: os.Stdout,
			SessionStarterFactory: func(loaded config.Config, selected config.Domain, path string) (app.SessionStarter, error) {
				deps, err := startDependencies(loaded, selected, path)
				if err != nil {
					return nil, err
				}
				deps.Host, deps.CA = processHost(), processCA()
				deps.Observer = observerFunc(func(_ context.Context, object string) (backend.Observation, error) {
					return backend.Observation{ObjectID: object, Exists: true, State: backend.ObjectStopped}, nil
				})
				return session.NewStartService(selected, deps), nil
			},
		})
		processExit(err)
	}
	os.Exit(m.Run())
}

func processExit(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(0)
}

func processHost() session.RuntimeChecker {
	return hostFunc(func(context.Context, hostx.Request) (hostx.RuntimeExpectation, error) {
		return hostx.RuntimeExpectation{}, nil
	})
}

func processCA() session.CAValidator {
	return caFunc(func(context.Context, sshx.Domain, []sshx.Domain) (sshx.CAIdentity, error) {
		return sshx.CAIdentity{Domain: "work", Algorithm: "ssh-ed25519", PublicKey: ownerTestPublicKey, Fingerprint: ownerTestFingerprint()}, nil
	})
}

func runProcessOwner(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var request supervisor.LaunchRequest
	if err := json.Unmarshal(data, &request); err != nil {
		return err
	}
	trace := &traceLog{}
	handle := &fakeHandle{trace: trace, done: make(chan struct{}), waiting: make(chan struct{})}
	var running atomic.Bool
	owner := NewOwner()
	owner.deps.host, owner.deps.ca = processHost(), processCA()
	owner.deps.observer = func(string, string) backend.Observer {
		return observerFunc(func(_ context.Context, object string) (backend.Observation, error) {
			state := backend.ObjectStopped
			if running.Load() {
				state = backend.ObjectRunning
			}
			select {
			case <-handle.done:
				state = backend.ObjectStopped
			default:
			}
			return backend.Observation{ObjectID: object, Exists: true, State: state}, nil
		})
	}
	owner.deps.launcher = func(tart.LaunchConfig) backend.Starter {
		return starterFunc(func(context.Context, backend.StartRequest) (backend.Handle, error) {
			running.Store(true)
			return handle, nil
		})
	}
	owner.deps.serial = func(_ context.Context, directory string) (serialRuntime, error) {
		dir := filepath.Join(directory, "serial")
		if err := os.Mkdir(dir, 0700); err != nil {
			return nil, err
		}
		return filesystemSerial{serialRuntime: &fakeSerial{trace: trace, endpoint: filepath.Join(dir, "tart-serial")}, directory: dir}, nil
	}
	// A failed test cannot leave an unbounded helper behind. The ordinary
	// successful path terminates earlier through the exact retained handle.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := supervisor.Run(ctx, path, owner); err != nil {
		return err
	}
	marker := request.HostConfigPath + ".reaped"
	temporary := marker + ".tmp"
	if err := os.WriteFile(temporary, []byte(strings.Join(trace.all(), ",")), 0600); err != nil {
		return err
	}
	return os.Rename(temporary, marker)
}

func TestInitiatingProcessReturnsWhileDetachedSupervisorRetainsRuntime(t *testing.T) {
	f := newFixture(t)
	f.record.IntendedState = session.StateStopped
	f.record.StartGeneration = ""
	f.record.Readiness = session.ReadinessRecord{Status: session.ReadinessNotReady}
	if err := session.SaveRecord(f.root, "work", f.record); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	initiator := exec.CommandContext(ctx, os.Args[0], "test-initiate", f.request.HostConfigPath)
	output, err := initiator.CombinedOutput()
	if err != nil {
		t.Fatalf("initiating process: %v\n%s", err, output)
	}
	if got, want := string(output), "domain: work\nsession: dev\nstate: starting\nreadiness: starting\n"; got != want {
		t.Fatalf("output = %q", got)
	}
	stored, err := session.LoadRecord(f.root, "work", "dev")
	if err != nil {
		t.Fatal(err)
	}
	if stored.IntendedState != session.StateStarting || stored.Readiness.Status != session.ReadinessStarting || len(stored.StartGeneration) != 36 {
		t.Fatalf("durable start = %#v", stored)
	}
	binding := f.request.Binding
	binding.Generation = stored.StartGeneration
	directory := filepath.Join(f.root, "runtime", binding.Domain, binding.SessionID, binding.Generation)
	client := &supervisor.Client{RuntimeDirectory: directory, MaxSnapshotAge: time.Minute}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		_ = client.Stop(ctx, binding)
	})
	snapshot, err := client.Snapshot(ctx, binding)
	if err != nil || snapshot.Binding != binding || !snapshot.BackendRunning || !snapshot.SerialHealthy || !snapshot.PinPresent || snapshot.CertificateCurrent || snapshot.ProbeOK || snapshot.ZoneMatches {
		t.Fatalf("post-initiator snapshot = %#v %v", snapshot, err)
	}
	assertGenerationLock(t, filepath.Join(directory, "generation.lock"), true)
	if _, err := os.Stat(filepath.Join(directory, "serial")); err != nil {
		t.Fatalf("runtime not retained: %v", err)
	}
	if err := client.Stop(ctx, binding); err != nil {
		t.Fatal(err)
	}
	var trace []byte
	for {
		trace, err = os.ReadFile(f.request.HostConfigPath + ".reaped")
		if err == nil {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("detached owner did not finish exact reap/cleanup")
		case <-time.After(10 * time.Millisecond):
		}
	}
	if string(trace) != "wait,stop,reap,close" {
		t.Fatalf("retained cleanup = %q", trace)
	}
	if _, err := os.Stat(directory); !os.IsNotExist(err) {
		t.Fatalf("exact generation retained after reap: %v", err)
	}
	after, err := session.LoadRecord(f.root, "work", "dev")
	if err != nil || after != stored {
		t.Fatalf("detached lifetime changed durable STARTING: %#v %v", after, err)
	}
}
