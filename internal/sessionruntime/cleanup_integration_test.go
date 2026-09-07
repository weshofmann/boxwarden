package sessionruntime

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/weshofmann/boxwarden/internal/backend"
	"github.com/weshofmann/boxwarden/internal/session"
	"github.com/weshofmann/boxwarden/internal/supervisor"
)

// Catch Run treating a returned startup error as unconditional cleanup
// authority even when the owner cannot prove the exact backend stopped.
func TestRunPreservesGenerationUnlessFailedStartProvesBackendStopped(t *testing.T) {
	for _, outcome := range []string{"observation error", "still running", "stopped"} {
		t.Run(outcome, func(t *testing.T) {
			f := newFixture(t)
			directory := f.request.RuntimeDirectory
			if err := os.MkdirAll(directory, 0700); err != nil {
				t.Fatal(err)
			}
			requestBytes, err := json.Marshal(f.request)
			if err != nil {
				t.Fatal(err)
			}
			requestPath := filepath.Join(directory, "supervisor-request.json")
			lockPath := filepath.Join(directory, "generation.lock")
			for path, contents := range map[string][]byte{requestPath: requestBytes, lockPath: nil} {
				if err := os.WriteFile(path, contents, 0600); err != nil {
					t.Fatal(err)
				}
			}
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
			err = supervisor.Run(context.Background(), requestPath, f.owner)
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
