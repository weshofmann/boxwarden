package supervisor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func exactCleanupResiduePath(request LaunchRequest) string {
	return filepath.Join(filepath.Dir(request.RuntimeDirectory), "."+request.Binding.Generation+".cleanup")
}

// Production break: a crash after cleanup starts must leave one exact state
// that the same durable generation can finish before it republishes itself.
// Otherwise starting + G becomes permanently unrecoverable after Tart is gone.
func TestPublishRecoversEveryInterruptedExactGenerationCleanupStage(t *testing.T) {
	tests := []struct {
		name       string
		durable    bool
		removeName string
		removeDir  bool
	}{
		{name: "after-rename-before-parent-fsync"},
		{name: "after-rename-publication-is-durable", durable: true},
		{name: "after-request-unlink", durable: true, removeName: requestName},
		{name: "after-lock-unlink", durable: true, removeName: lockName},
		{name: "after-directory-unlink-before-parent-fsync", durable: true, removeName: lockName, removeDir: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := minimalRequest(t)
			path, _, err := publishOrAdmitRequest(request)
			if err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			residue := exactCleanupResiduePath(request)
			if err := os.Rename(request.RuntimeDirectory, residue); err != nil {
				t.Fatal(err)
			}
			if test.durable {
				if err := syncDirectory(filepath.Dir(request.RuntimeDirectory)); err != nil {
					t.Fatal(err)
				}
			}
			if test.removeName != "" {
				if err := os.Remove(filepath.Join(residue, test.removeName)); err != nil {
					t.Fatal(err)
				}
			}
			if test.removeName == requestName {
				// Cleanup removes request before lock; this boundary is lock-only.
			} else if test.removeName == lockName {
				if err := os.Remove(filepath.Join(residue, requestName)); err != nil {
					t.Fatal(err)
				}
			}
			if test.removeDir {
				if err := os.Remove(residue); err != nil {
					t.Fatal(err)
				}
			}

			gotPath, first, err := publishOrAdmitRequest(request)
			if err != nil {
				t.Fatalf("same-G retry: %v", err)
			}
			if !first || gotPath != path {
				t.Fatalf("same-G retry publication = %q, %v; want %q, true", gotPath, first, path)
			}
			after, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(after, before) {
				t.Fatalf("republished request = %q, %v; want exact original", after, err)
			}
			if _, err := os.Lstat(residue); !os.IsNotExist(err) {
				t.Fatalf("cleanup residue retained beside republished G: %v", err)
			}
			if state, err := classifyExactGeneration(request); err != nil || state != exactGenerationResumable {
				t.Fatalf("republished generation state = %v, %v; want resumable", state, err)
			}
		})
	}
}

// Production break: the old supervisor holds generation.lock across the
// canonical-to-cleanup rename. A retry must contend on that same descriptor,
// never publish a second canonical G while cleanup is still active.
func TestPublishWaitsForExactCleanupOwnerBeforeRecoveringSameGeneration(t *testing.T) {
	request := minimalRequest(t)
	if _, _, err := publishOrAdmitRequest(request); err != nil {
		t.Fatal(err)
	}
	lock, err := acquireGenerationLock(request)
	if err != nil {
		t.Fatal(err)
	}
	residue := exactCleanupResiduePath(request)
	if err := os.Rename(request.RuntimeDirectory, residue); err != nil {
		lock.Close()
		t.Fatal(err)
	}
	if err := syncDirectory(filepath.Dir(request.RuntimeDirectory)); err != nil {
		lock.Close()
		t.Fatal(err)
	}

	if _, _, err := publishOrAdmitRequest(request); !errors.Is(err, errGenerationAlreadyOwned) {
		lock.Close()
		t.Fatalf("retry during active cleanup = %v; want exact ownership contention", err)
	}
	if _, err := os.Lstat(request.RuntimeDirectory); !os.IsNotExist(err) {
		lock.Close()
		t.Fatalf("retry raced active cleanup by publishing canonical G: %v", err)
	}
	if _, err := os.Lstat(residue); err != nil {
		lock.Close()
		t.Fatalf("retry mutated active cleanup residue: %v", err)
	}
	if err := lock.Close(); err != nil {
		t.Fatal(err)
	}

	if _, first, err := publishOrAdmitRequest(request); err != nil || !first {
		t.Fatalf("retry after cleanup-owner loss = %v, %v; want republished exact G", first, err)
	}
	if _, err := os.Lstat(residue); !os.IsNotExist(err) {
		t.Fatalf("completed cleanup residue retained: %v", err)
	}
}

type cleanupRetryLauncher struct {
	lock  *os.File
	calls atomic.Int32
}

func (l *cleanupRetryLauncher) Launch(_ context.Context, request LaunchRequest) error {
	l.calls.Add(1)
	_, _, err := publishOrAdmitRequest(request)
	if l.lock != nil {
		closeErr := l.lock.Close()
		l.lock = nil
		return errors.Join(err, closeErr)
	}
	return err
}

type cleanupRetryController struct{ request LaunchRequest }

func (c cleanupRetryController) Snapshot(_ context.Context, binding Binding) (Snapshot, error) {
	state, err := classifyExactGeneration(c.request)
	if err != nil || state != exactGenerationResumable {
		return Snapshot{}, errors.Join(fmt.Errorf("same-G publication is not resumable"), err)
	}
	return Snapshot{Binding: binding, BackendRunning: true, SerialHealthy: true}, nil
}

func (cleanupRetryController) Stop(context.Context, Binding) error { return nil }

// Production break: StartExact must retry publication after the finishing old
// supervisor releases the moved lock. Waiting only for a socket cannot work:
// cleanup deliberately removes that old supervisor and nobody else republishes G.
func TestExactStartRetriesSameGenerationAfterCleanupOwnerFinishes(t *testing.T) {
	request := minimalRequest(t)
	if _, _, err := publishOrAdmitRequest(request); err != nil {
		t.Fatal(err)
	}
	lock, err := acquireGenerationLock(request)
	if err != nil {
		t.Fatal(err)
	}
	residue := exactCleanupResiduePath(request)
	if err := os.Rename(request.RuntimeDirectory, residue); err != nil {
		lock.Close()
		t.Fatal(err)
	}
	if err := syncDirectory(filepath.Dir(request.RuntimeDirectory)); err != nil {
		lock.Close()
		t.Fatal(err)
	}
	launcher := &cleanupRetryLauncher{lock: lock}
	controller := &exactStartController{
		launcher:   launcher,
		controller: cleanupRetryController{request: request},
		policy:     startupPolicy{timeout: time.Second, interval: time.Millisecond},
	}

	got, err := controller.startExact(context.Background(), request)
	if err != nil {
		t.Fatalf("same-G retry: %v", err)
	}
	if got.Binding != request.Binding || !got.BackendRunning || !got.SerialHealthy {
		t.Fatalf("same-G snapshot = %#v", got)
	}
	if launcher.calls.Load() != 2 {
		t.Fatalf("launch calls = %d, want one contention plus one same-G republish", launcher.calls.Load())
	}
	if _, err := os.Lstat(residue); !os.IsNotExist(err) {
		t.Fatalf("cleanup residue retained after exact start retry: %v", err)
	}
}

var errInjectedCleanupInterruption = errors.New("injected cleanup interruption")

type interruptingCleanupOperations struct {
	kind, path string
	wantCall   int
	seen       int
}

func (o *interruptingCleanupOperations) Rename(oldPath, newPath string) error {
	if err := os.Rename(oldPath, newPath); err != nil {
		return err
	}
	return o.after("rename", newPath)
}

func (o *interruptingCleanupOperations) Remove(path string) error {
	if err := os.Remove(path); err != nil {
		return err
	}
	return o.after("remove", path)
}

func (o *interruptingCleanupOperations) SyncDirectory(path string) error {
	if err := syncDirectory(path); err != nil {
		return err
	}
	return o.after("sync", path)
}

func (o *interruptingCleanupOperations) after(kind, path string) error {
	if kind != o.kind || path != o.path {
		return nil
	}
	o.seen++
	if o.seen == o.wantCall {
		return errInjectedCleanupInterruption
	}
	return nil
}

// Production break: recovery tests alone would not catch removeExactGeneration
// reverting to partial canonical unlink. Inject an interruption after every
// externally distinguishable rename, unlink, and parent-fsync boundary and
// require the actual cleanup transaction to leave the same G recoverable.
func TestCleanupTransactionRecoversAfterEveryInjectedBoundary(t *testing.T) {
	for _, test := range []struct {
		name, kind string
		path       func(LaunchRequest) string
		wantCall   int
	}{
		{name: "after-generation-rename", kind: "rename", path: exactCleanupResiduePath, wantCall: 1},
		{name: "after-generation-rename-parent-fsync", kind: "sync", path: func(r LaunchRequest) string { return filepath.Dir(r.RuntimeDirectory) }, wantCall: 1},
		{name: "after-request-unlink", kind: "remove", path: func(r LaunchRequest) string { return filepath.Join(exactCleanupResiduePath(r), requestName) }, wantCall: 1},
		{name: "after-lock-unlink-to-marker", kind: "rename", path: func(r LaunchRequest) string { return exactCleanupResiduePath(r) + ".lock" }, wantCall: 1},
		{name: "after-empty-residue-fsync", kind: "sync", path: exactCleanupResiduePath, wantCall: 1},
		{name: "after-marker-publication-parent-fsync", kind: "sync", path: func(r LaunchRequest) string { return filepath.Dir(r.RuntimeDirectory) }, wantCall: 2},
		{name: "after-cleanup-directory-unlink", kind: "remove", path: exactCleanupResiduePath, wantCall: 1},
		{name: "after-cleanup-directory-parent-fsync", kind: "sync", path: func(r LaunchRequest) string { return filepath.Dir(r.RuntimeDirectory) }, wantCall: 3},
		{name: "after-lock-marker-unlink", kind: "remove", path: func(r LaunchRequest) string { return exactCleanupResiduePath(r) + ".lock" }, wantCall: 1},
		{name: "after-final-parent-fsync", kind: "sync", path: func(r LaunchRequest) string { return filepath.Dir(r.RuntimeDirectory) }, wantCall: 4},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := minimalRequest(t)
			path, _, err := publishOrAdmitRequest(request)
			if err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			lock, err := acquireGenerationLock(request)
			if err != nil {
				t.Fatal(err)
			}
			operations := &interruptingCleanupOperations{kind: test.kind, path: test.path(request), wantCall: test.wantCall}
			err = removeExactGenerationWithOperations(request, lock, operations)
			if !errors.Is(err, errInjectedCleanupInterruption) {
				lock.Close()
				t.Fatalf("cleanup error = %v; want injected interruption", err)
			}
			if operations.seen < operations.wantCall {
				lock.Close()
				t.Fatalf("cleanup never reached injected boundary: saw %d, want %d", operations.seen, operations.wantCall)
			}
			if _, err := os.Lstat(request.RuntimeDirectory); !os.IsNotExist(err) {
				lock.Close()
				t.Fatalf("interrupted cleanup left partially unlinked canonical G: %v", err)
			}
			if err := lock.Close(); err != nil {
				t.Fatal(err)
			}

			gotPath, first, err := publishOrAdmitRequest(request)
			if err != nil || !first || gotPath != path {
				t.Fatalf("same-G recovery publication = %q, %v, %v", gotPath, first, err)
			}
			after, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(after, before) {
				t.Fatalf("same-G request changed after recovery: %q, %v", after, err)
			}
			for _, residue := range []string{exactCleanupResiduePath(request), exactCleanupResiduePath(request) + ".lock"} {
				if _, err := os.Lstat(residue); !os.IsNotExist(err) {
					t.Fatalf("cleanup residue retained after recovery at %s: %v", residue, err)
				}
			}
		})
	}
}

// Production break: a deterministic name is correlation, not deletion
// authority. Only exact transaction stages may be completed, and coexistence
// with canonical G is ambiguous rather than a reason to delete either tree.
func TestPublishRejectsInvalidExactCleanupResidueWithoutMutation(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(t *testing.T, request LaunchRequest, residue string)
	}{
		{name: "canonical-coexists", mutate: func(t *testing.T, request LaunchRequest, residue string) {
			t.Helper()
			copyExactGenerationForCleanupTest(t, request, residue)
		}},
		{name: "foreign-request", mutate: func(t *testing.T, request LaunchRequest, residue string) {
			t.Helper()
			foreign := request
			foreign.Binding.BackendObject = "foreign"
			data, err := json.Marshal(foreign)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(residue, requestName), data, 0600); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "malformed-request", mutate: func(t *testing.T, _ LaunchRequest, residue string) {
			t.Helper()
			if err := os.WriteFile(filepath.Join(residue, requestName), []byte("{"), 0600); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "request-only-invalid-stage", mutate: func(t *testing.T, _ LaunchRequest, residue string) {
			t.Helper()
			if err := os.Remove(filepath.Join(residue, lockName)); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "nonempty-lock", mutate: func(t *testing.T, _ LaunchRequest, residue string) {
			t.Helper()
			if err := os.WriteFile(filepath.Join(residue, lockName), []byte("foreign"), 0600); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "unexpected-entry", mutate: func(t *testing.T, _ LaunchRequest, residue string) {
			t.Helper()
			if err := os.WriteFile(filepath.Join(residue, "foreign"), []byte("retain"), 0600); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "symlinked-lock", mutate: func(t *testing.T, _ LaunchRequest, residue string) {
			t.Helper()
			if err := os.Remove(filepath.Join(residue, lockName)); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(filepath.Join(residue, requestName), filepath.Join(residue, lockName)); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "public-residue", mutate: func(t *testing.T, _ LaunchRequest, residue string) {
			t.Helper()
			if err := os.Chmod(residue, 0755); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "symlinked-residue", mutate: func(t *testing.T, _ LaunchRequest, residue string) {
			t.Helper()
			real := residue + ".real"
			if err := os.Rename(residue, real); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(real, residue); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := minimalRequest(t)
			if _, _, err := publishOrAdmitRequest(request); err != nil {
				t.Fatal(err)
			}
			residue := exactCleanupResiduePath(request)
			if test.name == "canonical-coexists" {
				test.mutate(t, request, residue)
			} else {
				if err := os.Rename(request.RuntimeDirectory, residue); err != nil {
					t.Fatal(err)
				}
				test.mutate(t, request, residue)
			}
			before := exactCleanupStateForTest(t, request, residue)

			if _, _, err := publishOrAdmitRequest(request); err == nil {
				t.Fatal("invalid cleanup residue was admitted or removed")
			}
			after := exactCleanupStateForTest(t, request, residue)
			if before != after {
				t.Fatalf("invalid cleanup residue mutated\nbefore: %s\nafter:  %s", before, after)
			}
		})
	}
}

func copyExactGenerationForCleanupTest(t *testing.T, request LaunchRequest, residue string) {
	t.Helper()
	if err := os.Mkdir(residue, 0700); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(request.RuntimeDirectory, requestName))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(residue, requestName), data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(residue, lockName), nil, 0600); err != nil {
		t.Fatal(err)
	}
}

func exactCleanupStateForTest(t *testing.T, request LaunchRequest, residue string) string {
	t.Helper()
	var state strings.Builder
	for _, path := range []string{request.RuntimeDirectory, residue, residue + ".real"} {
		info, err := os.Lstat(path)
		if os.IsNotExist(err) {
			state.WriteString(path + "=absent\n")
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		state.WriteString(path + "=" + info.Mode().String() + "\n")
		if info.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(path)
			if err != nil {
				t.Fatal(err)
			}
			state.WriteString("target=" + target + "\n")
			continue
		}
		if !info.IsDir() {
			continue
		}
		entries, err := os.ReadDir(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			entryInfo, err := os.Lstat(filepath.Join(path, entry.Name()))
			if err != nil {
				t.Fatal(err)
			}
			state.WriteString(entry.Name() + "=" + entryInfo.Mode().String())
			if entryInfo.Mode().IsRegular() {
				data, err := os.ReadFile(filepath.Join(path, entry.Name()))
				if err != nil {
					t.Fatal(err)
				}
				state.WriteString("=" + string(data))
			} else if entryInfo.Mode()&os.ModeSymlink != 0 {
				target, err := os.Readlink(filepath.Join(path, entry.Name()))
				if err != nil {
					t.Fatal(err)
				}
				state.WriteString("=" + target)
			}
			state.WriteByte('\n')
		}
	}
	return state.String()
}
