package supervisor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
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
		{name: "after-cleanup-completion-before-parent-fsync", durable: true, removeName: lockName, removeDir: true},
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

// Production break: an unsynced request unlink followed by lock-to-marker
// rename can recover as request+marker after a crash. The marker is still the
// exact ownership lock, so this state must contend while live and replay once
// released instead of becoming permanent drift.
func TestPublishRecoversExactRequestAndMarkerCleanupStage(t *testing.T) {
	request := minimalRequest(t)
	path, _, err := publishOrAdmitRequest(request)
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lock, residue, marker := makeRequestAndMarkerCleanupStage(t, request)
	activeState := exactCleanupStateForTest(t, request, residue)

	if _, _, err := publishOrAdmitRequest(request); !errors.Is(err, errGenerationAlreadyOwned) {
		t.Errorf("retry during active request+marker cleanup = %v; want exact ownership contention", err)
	}
	if after := exactCleanupStateForTest(t, request, residue); after != activeState {
		t.Fatalf("active request+marker cleanup mutated\nbefore: %s\nafter:  %s", activeState, after)
	}
	if err := lock.Close(); err != nil {
		t.Fatal(err)
	}

	gotPath, first, err := publishOrAdmitRequest(request)
	if err != nil {
		t.Fatalf("same-G request+marker retry: %v", err)
	}
	if !first || gotPath != path {
		t.Fatalf("same-G request+marker publication = %q, %v; want %q, true", gotPath, first, path)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(after, before) {
		t.Fatalf("republished request = %q, %v; want exact original", after, err)
	}
	for _, cleanupPath := range []string{residue, marker} {
		if _, err := os.Lstat(cleanupPath); !os.IsNotExist(err) {
			t.Fatalf("request+marker cleanup residue retained at %s: %v", cleanupPath, err)
		}
	}
}

func TestPublishRejectsForeignOrMalformedRequestAndMarkerWithoutMutation(t *testing.T) {
	for _, test := range []struct {
		name string
		data func(LaunchRequest) []byte
	}{
		{name: "foreign", data: func(request LaunchRequest) []byte {
			request.Binding.BackendObject = "foreign"
			data, _ := json.Marshal(request)
			return data
		}},
		{name: "malformed", data: func(LaunchRequest) []byte { return []byte("{") }},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := minimalRequest(t)
			if _, _, err := publishOrAdmitRequest(request); err != nil {
				t.Fatal(err)
			}
			lock, residue, _ := makeRequestAndMarkerCleanupStage(t, request)
			if err := lock.Close(); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(residue, requestName), test.data(request), 0600); err != nil {
				t.Fatal(err)
			}
			before := exactCleanupStateForTest(t, request, residue)

			if _, _, err := publishOrAdmitRequest(request); err == nil {
				t.Fatal("foreign or malformed request+marker cleanup was admitted")
			}
			if after := exactCleanupStateForTest(t, request, residue); after != before {
				t.Fatalf("foreign or malformed request+marker mutated\nbefore: %s\nafter:  %s", before, after)
			}
		})
	}
}

func makeRequestAndMarkerCleanupStage(t *testing.T, request LaunchRequest) (*os.File, string, string) {
	t.Helper()
	lock, err := acquireGenerationLock(request)
	if err != nil {
		t.Fatal(err)
	}
	residue := exactCleanupResiduePath(request)
	marker := residue + ".lock"
	if err := os.Rename(request.RuntimeDirectory, residue); err != nil {
		lock.Close()
		t.Fatal(err)
	}
	if err := syncDirectory(filepath.Dir(request.RuntimeDirectory)); err != nil {
		lock.Close()
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(residue, lockName), marker); err != nil {
		lock.Close()
		t.Fatal(err)
	}
	return lock, residue, marker
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

type liveCleanupTransitionLauncher struct {
	want     LaunchRequest
	lock     *os.File
	lockInfo os.FileInfo
	calls    atomic.Int32
	seen     []LaunchRequest
}

func (l *liveCleanupTransitionLauncher) Launch(_ context.Context, request LaunchRequest) error {
	l.seen = append(l.seen, request)
	if request != l.want {
		return fmt.Errorf("launch request changed across exact-generation retry")
	}
	call := l.calls.Add(1)
	_, _, err := publishOrAdmitRequest(request)
	if call != 1 {
		return err
	}
	if !errors.Is(err, errGenerationAlreadyOwned) {
		return fmt.Errorf("first retry did not contend with cleanup owner: %w", err)
	}
	cleanupErr := finishExactGenerationCleanup(request, l.lockInfo)
	closeErr := l.lock.Close()
	l.lock = nil
	if cleanupErr != nil || closeErr != nil {
		return errors.Join(cleanupErr, closeErr)
	}
	return err
}

type liveCleanupTransitionController struct {
	request  LaunchRequest
	launcher *liveCleanupTransitionLauncher
	calls    atomic.Int32
}

func (c *liveCleanupTransitionController) Snapshot(_ context.Context, binding Binding) (Snapshot, error) {
	if binding != c.request.Binding {
		return Snapshot{}, fmt.Errorf("snapshot binding changed across exact-generation retry")
	}
	if c.calls.Add(1) == 1 {
		if err := os.Rename(c.request.RuntimeDirectory, exactCleanupResiduePath(c.request)); err != nil {
			return Snapshot{}, err
		}
		if err := syncDirectory(filepath.Dir(c.request.RuntimeDirectory)); err != nil {
			return Snapshot{}, err
		}
		return Snapshot{}, fmt.Errorf("old exact supervisor entered cleanup")
	}
	if c.launcher.calls.Load() < 2 {
		return Snapshot{}, fmt.Errorf("same generation has not been republished")
	}
	state, err := classifyExactGeneration(c.request)
	if err != nil || state != exactGenerationResumable {
		return Snapshot{}, errors.Join(fmt.Errorf("republished generation is not resumable"), err)
	}
	return Snapshot{Binding: binding, BackendRunning: true, SerialHealthy: true}, nil
}

func (*liveCleanupTransitionController) Stop(context.Context, Binding) error { return nil }

// Production break: an exact retry that first finds the old supervisor live
// must not spend its entire startup budget polling a socket after that owner
// enters cleanup. It must re-enter exact admission and republish only the same G.
func TestExactStartReentersSameGenerationAfterLiveOwnerCleansUp(t *testing.T) {
	request := minimalRequest(t)
	if _, _, err := publishOrAdmitRequest(request); err != nil {
		t.Fatal(err)
	}
	lock, err := acquireGenerationLock(request)
	if err != nil {
		t.Fatal(err)
	}
	lockInfo, err := lock.Stat()
	if err != nil {
		lock.Close()
		t.Fatal(err)
	}
	launcher := &liveCleanupTransitionLauncher{want: request, lock: lock, lockInfo: lockInfo}
	defer func() {
		if launcher.lock != nil {
			launcher.lock.Close()
		}
	}()
	controller := &liveCleanupTransitionController{request: request, launcher: launcher}
	exact := &exactStartController{
		launcher:   launcher,
		controller: controller,
		policy:     startupPolicy{timeout: time.Second, interval: time.Millisecond},
	}

	got, err := exact.startExact(context.Background(), request)
	if err != nil {
		t.Fatalf("live-to-cleanup exact retry: %v", err)
	}
	if got.Binding != request.Binding || !got.BackendRunning || !got.SerialHealthy {
		t.Fatalf("fresh same-G snapshot = %#v", got)
	}
	if launcher.calls.Load() != 2 {
		t.Fatalf("launch calls = %d, want cleanup contention plus exact republish", launcher.calls.Load())
	}
	for i, seen := range launcher.seen {
		if seen != request {
			t.Fatalf("launch request %d = %#v, want exact %#v", i, seen, request)
		}
	}
	if controller.calls.Load() != 2 {
		t.Fatalf("snapshot calls = %d, want old transition plus fresh same-G snapshot", controller.calls.Load())
	}
	if _, err := os.Lstat(exactCleanupResiduePath(request)); !os.IsNotExist(err) {
		t.Fatalf("cleanup residue retained after exact relaunch: %v", err)
	}
}

type coexistingCleanupController struct {
	request LaunchRequest
	calls   atomic.Int32
}

func (c *coexistingCleanupController) Snapshot(_ context.Context, binding Binding) (Snapshot, error) {
	if binding != c.request.Binding {
		return Snapshot{}, fmt.Errorf("snapshot binding changed during ambiguous transition")
	}
	if c.calls.Add(1) == 1 {
		residue := exactCleanupResiduePath(c.request)
		if err := os.Mkdir(residue, 0700); err != nil {
			return Snapshot{}, err
		}
		data, err := os.ReadFile(filepath.Join(c.request.RuntimeDirectory, requestName))
		if err != nil {
			return Snapshot{}, err
		}
		if err := os.WriteFile(filepath.Join(residue, requestName), data, 0600); err != nil {
			return Snapshot{}, err
		}
		if err := os.WriteFile(filepath.Join(residue, lockName), nil, 0600); err != nil {
			return Snapshot{}, err
		}
	}
	return Snapshot{}, fmt.Errorf("old exact supervisor entered ambiguous state")
}

func (*coexistingCleanupController) Stop(context.Context, Binding) error { return nil }

func TestExactStartFailsClosedWhenLiveGenerationBecomesAmbiguous(t *testing.T) {
	request := minimalRequest(t)
	if _, _, err := publishOrAdmitRequest(request); err != nil {
		t.Fatal(err)
	}
	lock, err := acquireGenerationLock(request)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	launcher := &rejectLauncher{}
	controller := &coexistingCleanupController{request: request}
	exact := &exactStartController{
		launcher:   launcher,
		controller: controller,
		policy:     startupPolicy{timeout: 200 * time.Millisecond, interval: time.Millisecond},
	}

	_, err = exact.startExact(context.Background(), request)
	if err == nil || !strings.Contains(err.Error(), "canonical generation and exact cleanup residue coexist") {
		t.Fatalf("ambiguous live transition error = %v, want exact fail-closed classification", err)
	}
	if launcher.calls.Load() != 0 {
		t.Fatal("ambiguous live transition reached launcher")
	}
	for _, path := range []string{request.RuntimeDirectory, exactCleanupResiduePath(request)} {
		if _, err := os.Lstat(path); err != nil {
			t.Fatalf("ambiguous live transition mutated %s: %v", path, err)
		}
	}
}

func TestDetachedLauncherReportsLiveGenerationTransitionForExactRetry(t *testing.T) {
	request := minimalRequest(t)
	if _, _, err := publishOrAdmitRequest(request); err != nil {
		t.Fatal(err)
	}
	lock, err := acquireGenerationLock(request)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	socketPath := filepath.Join(request.RuntimeDirectory, socketName)
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: socketPath, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	listener.SetUnlinkOnClose(true)
	defer listener.Close()
	if err := os.Chmod(socketPath, 0600); err != nil {
		t.Fatal(err)
	}
	accepted := make(chan *net.UnixConn, 1)
	acceptErr := make(chan error, 1)
	go func() {
		connection, err := listener.AcceptUnix()
		if err != nil {
			acceptErr <- err
			return
		}
		if _, err := readBounded(connection); err != nil {
			connection.Close()
			acceptErr <- err
			return
		}
		accepted <- connection
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	launchDone := make(chan error, 1)
	go func() { launchDone <- (detachedLauncher{}).Launch(ctx, request) }()
	var connection *net.UnixConn
	select {
	case connection = <-accepted:
	case err := <-acceptErr:
		t.Fatalf("accept live snapshot request: %v", err)
	case <-ctx.Done():
		t.Fatal("detached launcher did not contact the live generation")
	}
	if err := listener.Close(); err != nil {
		connection.Close()
		t.Fatal(err)
	}
	if err := os.Rename(request.RuntimeDirectory, exactCleanupResiduePath(request)); err != nil {
		connection.Close()
		t.Fatal(err)
	}
	if err := syncDirectory(filepath.Dir(request.RuntimeDirectory)); err != nil {
		connection.Close()
		t.Fatal(err)
	}
	if err := connection.Close(); err != nil {
		t.Fatal(err)
	}
	if state, err := classifyExactGeneration(request); err != nil || state != exactGenerationAbsent {
		t.Fatalf("detached winner cleanup state = %v, %v; want exact transition", state, err)
	}
	select {
	case err := <-launchDone:
		if !errors.Is(err, errExactGenerationTransition) {
			t.Fatalf("detached live transition = %v, want exact retry signal", err)
		}
	case <-time.After(1500 * time.Millisecond):
		t.Fatal("detached launcher hid the live-generation transition until timeout")
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
		name, kind    string
		path          func(LaunchRequest) string
		wantCall      int
		wantPreMarker bool
	}{
		{name: "after-generation-rename", kind: "rename", path: exactCleanupResiduePath, wantCall: 1},
		{name: "after-generation-rename-parent-fsync", kind: "sync", path: func(r LaunchRequest) string { return filepath.Dir(r.RuntimeDirectory) }, wantCall: 1},
		{name: "after-request-unlink", kind: "remove", path: func(r LaunchRequest) string { return filepath.Join(exactCleanupResiduePath(r), requestName) }, wantCall: 1},
		{name: "after-request-removal-directory-fsync", kind: "sync", path: exactCleanupResiduePath, wantCall: 1, wantPreMarker: true},
		{name: "after-lock-unlink-to-marker", kind: "rename", path: func(r LaunchRequest) string { return exactCleanupResiduePath(r) + ".lock" }, wantCall: 1},
		{name: "after-empty-residue-fsync", kind: "sync", path: exactCleanupResiduePath, wantCall: 2},
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
			if test.wantPreMarker {
				if _, err := os.Lstat(filepath.Join(exactCleanupResiduePath(request), lockName)); err != nil {
					lock.Close()
					t.Fatalf("pre-marker sync did not retain the in-directory lock: %v", err)
				}
				if _, err := os.Lstat(exactCleanupLockMarker(request)); !os.IsNotExist(err) {
					lock.Close()
					t.Fatalf("pre-marker sync published the marker early: %v", err)
				}
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
		{name: "ownerless-empty-directory", mutate: func(t *testing.T, _ LaunchRequest, residue string) {
			t.Helper()
			for _, name := range []string{requestName, lockName} {
				if err := os.Remove(filepath.Join(residue, name)); err != nil {
					t.Fatal(err)
				}
			}
		}},
		{name: "duplicate-lock-paths", mutate: func(t *testing.T, _ LaunchRequest, residue string) {
			t.Helper()
			if err := os.WriteFile(residue+".lock", nil, 0600); err != nil {
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

			_, _, publishErr := publishOrAdmitRequest(request)
			after := exactCleanupStateForTest(t, request, residue)
			if before != after {
				t.Fatalf("invalid cleanup residue mutated\nbefore: %s\nafter:  %s", before, after)
			}
			if publishErr == nil {
				t.Fatal("invalid cleanup residue was admitted")
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
	for _, path := range []string{request.RuntimeDirectory, residue, residue + ".lock", residue + ".real"} {
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
