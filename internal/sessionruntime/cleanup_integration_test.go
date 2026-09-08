package sessionruntime

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/weshofmann/boxwarden/internal/backend"
	"github.com/weshofmann/boxwarden/internal/session"
	"github.com/weshofmann/boxwarden/internal/supervisor"
)

// Production break: an abandoned snapshot RPC must cancel its exact backend
// observation. Otherwise the serialized control loop remains stuck behind work
// whose client has gone away and cannot deliver the exact stop to the retained
// handle.
func TestAbandonedSnapshotObservationDoesNotStarveExactStop(t *testing.T) {
	f := newFixture(t)
	directory := f.request.RuntimeDirectory
	requestPath, _ := writeExactGeneration(t, f.request)

	var observations atomic.Int32
	observing, observationCanceled := make(chan struct{}), make(chan struct{})
	f.observe = func(ctx context.Context, object string) (backend.Observation, error) {
		call := observations.Add(1)
		switch call {
		case 1:
			return backend.Observation{ObjectID: object, Exists: true, State: backend.ObjectStopped}, nil
		case 2:
			return backend.Observation{ObjectID: object, Exists: true, State: backend.ObjectRunning}, nil
		case 3:
			close(observing)
			<-ctx.Done()
			close(observationCanceled)
			return backend.Observation{}, ctx.Err()
		default:
			<-ctx.Done()
			return backend.Observation{}, ctx.Err()
		}
	}
	runDone := make(chan error, 1)
	go func() { runDone <- supervisor.Run(context.Background(), requestPath, f.owner) }()
	waitForControlSocket(t, filepath.Join(directory, "supervisor.sock"))

	client := &supervisor.Client{RuntimeDirectory: directory, MaxSnapshotAge: time.Minute}
	snapshotDone := make(chan error, 1)
	go func() {
		_, err := client.Snapshot(context.Background(), f.request.Binding)
		snapshotDone <- err
	}()
	select {
	case <-observing:
	case err := <-snapshotDone:
		t.Fatalf("snapshot RPC ended before reaching the blocking observation: %v", err)
	case <-time.After(3 * time.Second):
		t.Fatal("snapshot never reached the blocking backend observation")
	}
	const staleSnapshots = 3
	staleDone := make(chan error, staleSnapshots)
	for range staleSnapshots {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
			defer cancel()
			_, err := client.Snapshot(ctx, f.request.Binding)
			staleDone <- err
		}()
	}
	for range staleSnapshots {
		select {
		case err := <-staleDone:
			requireReadTimeout(t, err)
		case <-time.After(2 * time.Second):
			t.Fatal("queued snapshot did not expire behind the active request")
		}
	}
	select {
	case err := <-snapshotDone:
		requireReadTimeout(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("snapshot client did not enforce its RPC budget")
	}

	stopCtx, cancelStop := context.WithTimeout(context.Background(), time.Second)
	err := client.Stop(stopCtx, f.request.Binding)
	cancelStop()
	if err != nil {
		t.Errorf("exact stop sat behind abandoned snapshot work: %v", err)
	}
	select {
	case <-observationCanceled:
	default:
		t.Error("exact stop returned before the abandoned observation released")
	}
	select {
	case err := <-runDone:
		if err != nil {
			t.Errorf("supervisor run after exact stop: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("supervisor did not finish after exact stop")
	}

	events := strings.Join(f.trace.all(), ",")
	for _, event := range []string{"stop", "wait", "reap", "close"} {
		if strings.Count(events, event) != 1 {
			t.Errorf("%s count in %q = %d, want 1", event, events, strings.Count(events, event))
		}
	}
	if observations.Load() != 3 {
		t.Errorf("backend observations = %d, want admission, startup, and abandoned snapshot only", observations.Load())
	}
	if _, err := os.Lstat(directory); !os.IsNotExist(err) {
		t.Errorf("exact generation retained after stop/reap: %v", err)
	}
}

func requireReadTimeout(t *testing.T, err error) {
	t.Helper()
	var networkError *net.OpError
	if !errors.As(err, &networkError) || networkError.Op != "read" || !networkError.Timeout() {
		t.Fatalf("snapshot RPC error = %v, want read timeout after a fully written request", err)
	}
}

func waitForControlSocket(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for {
		if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSocket != 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("control socket did not appear at %s", path)
		}
		time.Sleep(time.Millisecond)
	}
}

// Catch Run treating a returned startup error as unconditional cleanup
// authority even when the owner cannot prove the exact backend stopped.
func TestRunPreservesGenerationUnlessFailedStartProvesBackendStopped(t *testing.T) {
	for _, outcome := range []string{"observation error", "still running", "stopped"} {
		t.Run(outcome, func(t *testing.T) {
			f := newFixture(t)
			directory := f.request.RuntimeDirectory
			requestPath, requestBytes := writeExactGeneration(t, f.request)
			lockPath := filepath.Join(directory, "generation.lock")
			serialDirectory := filepath.Join(directory, "serial")
			f.owner.deps.serial = func(context.Context, string) (serialRuntime, error) {
				f.trace.add("serial")
				if err := os.Mkdir(serialDirectory, 0700); err != nil {
					return nil, err
				}
				return filesystemSerial{serialRuntime: f.serial, directory: serialDirectory}, nil
			}
			observations := 0
			f.observe = func(_ context.Context, object string) (backend.Observation, error) {
				observations++
				observation := backend.Observation{ObjectID: object, Exists: true, State: backend.ObjectStopped}
				switch observations {
				case 1:
					return observation, nil
				case 2:
					return backend.Observation{}, errors.New("startup observation failed")
				case 3:
					if _, err := os.Lstat(serialDirectory); !os.IsNotExist(err) {
						t.Fatalf("final proof ran before serial cleanup: %v", err)
					}
					assertGenerationLock(t, lockPath, true)
					if outcome == "observation error" {
						return backend.Observation{}, errors.New("final observation unavailable")
					}
					if outcome == "still running" {
						observation.State = backend.ObjectRunning
					}
					return observation, nil
				default:
					t.Fatalf("unexpected observation %d", observations)
					return backend.Observation{}, nil
				}
			}
			err := supervisor.Run(context.Background(), requestPath, f.owner)
			if err == nil || !strings.Contains(err.Error(), "startup observation failed") {
				t.Fatalf("Run did not reach expected post-handle failure: %v", err)
			}
			if got := errors.Is(err, supervisor.ErrRuntimeCleanupUnproven); got != (outcome != "stopped") {
				t.Fatalf("cleanup-unproven marker = %v for outcome %q: %v", got, outcome, err)
			}
			if observations != 3 {
				t.Fatalf("observations = %d, want admission/start/final proof", observations)
			}
			if events := strings.Join(f.trace.all(), ","); !strings.HasSuffix(events, "stop,wait,reap,close,observe:boxwarden-work-dev") {
				t.Fatalf("cleanup order = %s", events)
			}
			stored, err := session.LoadRecord(f.root, "work", "dev")
			if err != nil || stored != f.record {
				t.Fatalf("durable starting binding changed: %#v, %v", stored, err)
			}
			if outcome == "stopped" {
				if _, err := os.Lstat(directory); !os.IsNotExist(err) {
					t.Fatalf("fully cleaned startup failure retained generation: %v", err)
				}
				return
			}
			entries, err := os.ReadDir(directory)
			if err != nil {
				t.Fatalf("unproven stopped state erased exact generation: %v", err)
			}
			if len(entries) != 2 || entries[0].Name() != "generation.lock" || entries[1].Name() != "supervisor-request.json" {
				t.Fatalf("preserved generation entries = %v", entries)
			}
			gotRequest, err := os.ReadFile(requestPath)
			if err != nil || string(gotRequest) != string(requestBytes) {
				t.Fatalf("preserved exact request changed: %q, %v", gotRequest, err)
			}
			assertGenerationLock(t, lockPath, false)
		})
	}
}

func writeExactGeneration(t *testing.T, request supervisor.LaunchRequest) (string, []byte) {
	t.Helper()
	if err := os.MkdirAll(request.RuntimeDirectory, 0700); err != nil {
		t.Fatal(err)
	}
	requestBytes, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	requestPath := filepath.Join(request.RuntimeDirectory, "supervisor-request.json")
	if err := os.WriteFile(requestPath, requestBytes, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(request.RuntimeDirectory, "generation.lock"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	return requestPath, requestBytes
}

// The fake owns a real serial directory so Run could remove the now-empty
// generation if it mistakenly treated the final proof error as cleanup success.
type filesystemSerial struct {
	serialRuntime
	directory string
}

func (s filesystemSerial) Close() error {
	return errors.Join(s.serialRuntime.Close(), os.Remove(s.directory))
}

func assertGenerationLock(t *testing.T, path string, held bool) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if held {
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) {
			t.Fatalf("generation lock not held through final proof: %v", err)
		}
	} else if err != nil {
		t.Fatalf("generation lock not released after Run returned: %v", err)
	}
}
