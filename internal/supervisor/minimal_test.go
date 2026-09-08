package supervisor

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"
)

func minimalRequest(t *testing.T) LaunchRequest {
	t.Helper()
	parent, err := filepath.EvalSymlinks(os.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// Darwin has the smaller sockaddr_un path capacity (104 bytes). Its OS
	// temp directory can be too long even before adding the generation tree.
	const socketSuffix = "/bw-4294967295/personal/session-1/generation-1/supervisor.sock"
	if len(parent)+len(socketSuffix) >= 104 {
		parent, err = filepath.EvalSymlinks("/tmp")
		if err != nil {
			t.Fatal(err)
		}
	}
	root, err := os.MkdirTemp(parent, "bw-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	return LaunchRequest{Binding: Binding{Domain: "personal", SessionID: "session-1", BackendKind: "tart", BackendObject: "vm-1", Generation: "generation-1"}, RuntimeDirectory: filepath.Join(root, "personal", "session-1", "generation-1"), HostConfigPath: "/private/config.json", SessionRecordName: "dev"}
}

func TestRequestFixtureUsesCanonicalTemporaryRoot(t *testing.T) {
	base, err := os.MkdirTemp("/tmp", "bw-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(base) })
	canonical, err := filepath.EvalSymlinks(base)
	if err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(base, "alias")
	if err := os.Symlink(canonical, alias); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMPDIR", alias)
	request := minimalRequest(t)
	if !strings.HasPrefix(request.RuntimeDirectory, canonical+string(os.PathSeparator)) {
		t.Fatalf("fixture ignored OS temp root: %s", request.RuntimeDirectory)
	}
	if _, _, err := publishOrAdmitRequest(request); err != nil {
		t.Fatalf("canonical fixture rejected: %v", err)
	}
}

func TestRequestFixturePreservesSocketHeadroomWithLongTempRoot(t *testing.T) {
	base := t.TempDir()
	long := filepath.Join(base, strings.Repeat("x", 100))
	if err := os.Mkdir(long, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMPDIR", long)
	request := minimalRequest(t)
	if _, _, err := publishOrAdmitRequest(request); err != nil {
		t.Fatal(err)
	}
	listener, err := listenSocket(filepath.Join(request.RuntimeDirectory, socketName))
	if err != nil {
		t.Fatalf("fixture leaves no Unix socket headroom: %v", err)
	}
	listener.Close()
}

func TestSingleGenerationLockWinnerAndCrashRetry(t *testing.T) {
	request := minimalRequest(t)
	if _, _, err := publishOrAdmitRequest(request); err != nil {
		t.Fatal(err)
	}
	first, err := acquireGenerationLock(request)
	if err != nil {
		t.Fatal(err)
	}
	if second, err := acquireGenerationLock(request); !errors.Is(err, errGenerationAlreadyOwned) {
		if second != nil {
			second.Close()
		}
		t.Fatalf("second owner: %v", err)
	}
	if state, err := classifyExactGeneration(request); err != nil || state != exactGenerationLive {
		t.Fatalf("held lock state %v: %v", state, err)
	}
	first.Close()
	if state, err := classifyExactGeneration(request); err != nil || state != exactGenerationResumable {
		t.Fatalf("released lock state %v: %v", state, err)
	}
}

// Production break: requiring future READY predicates here would keep Slice B
// start blocked after the exact backend and serial drain are already healthy.
func TestAwaitSnapshotReturnsAtBackendPlusSerialStartedBoundary(t *testing.T) {
	binding := minimalRequest(t).Binding
	snapshot := Snapshot{Binding: binding, BackendRunning: true, SerialHealthy: true, ObservedAt: time.Now().UTC()}
	got, err := awaitSnapshot(context.Background(), binding, startupPolicy{timeout: 20 * time.Millisecond, interval: time.Millisecond}, func(context.Context, Binding) (Snapshot, error) {
		return snapshot, nil
	})
	if err != nil {
		t.Fatalf("awaitSnapshot() error = %v", err)
	}
	if got != snapshot {
		t.Fatalf("awaitSnapshot() = %#v, want %#v", got, snapshot)
	}
	if snapshotReady(snapshot) {
		t.Fatal("full snapshotReady accepted a Slice B-only started snapshot")
	}
}

func TestTypedControlBounds(t *testing.T) {
	for _, size := range []uint32{0, maxControlBytes + 1, ^uint32(0)} {
		var wire bytes.Buffer
		binary.Write(&wire, binary.BigEndian, size)
		if _, err := readBounded(&wire); err == nil {
			t.Fatalf("accepted frame size %d", size)
		}
	}
	if err := writeFrame(&bytes.Buffer{}, make([]byte, maxControlBytes+1)); err == nil {
		t.Fatal("wrote oversized frame")
	}
	for _, data := range []string{`{"version":1,"version":1}`, `{"command":"exec"}`, `{} {}`} {
		var request controlRequest
		if err := decodeExact([]byte(data), &request); err == nil {
			t.Fatalf("accepted %s", data)
		}
	}
}

type diagnosticRuntime struct {
	runtimeFixture
	diagnostic string
}

func (o *diagnosticRuntime) Snapshot(ctx context.Context) Snapshot {
	s := o.runtimeFixture.Snapshot(ctx)
	s.Diagnostic = o.diagnostic
	return s
}

func TestControlReturnsEncodedBoundedUTF8Diagnostics(t *testing.T) {
	for _, text := range []string{strings.Repeat("\x01", maxDiagnosticBytes), strings.Repeat("界", maxDiagnosticBytes)} {
		t.Run(fmt.Sprintf("rune-%U", []rune(text)[0]), func(t *testing.T) {
			binding := minimalRequest(t).Binding
			owner := &diagnosticRuntime{runtimeFixture: runtimeFixture{binding: binding, done: make(chan struct{})}, diagnostic: text}
			server, client := net.Pipe()
			defer client.Close()
			served := make(chan struct{})
			go func() {
				handleControl(context.Background(), server, binding, owner, func() error { owner.stops.Add(1); return errors.New(text) })
				close(served)
			}()
			client.SetDeadline(time.Now().Add(time.Second))
			request, _ := json.Marshal(controlRequest{Version: 1, Action: "stop", Binding: binding})
			if err := writeFrame(client, request); err != nil {
				t.Fatal(err)
			}
			data, err := readBounded(client)
			if err != nil {
				t.Fatalf("stop executed but no bounded response returned: %v", err)
			}
			<-served
			var response controlResponse
			if err := decodeExact(data, &response); err != nil {
				t.Fatal(err)
			}
			if response.Binding != binding || response.Snapshot.Binding != binding || owner.stops.Load() != 1 {
				t.Fatal("response lost exact stop binding")
			}
			for _, diagnostic := range []string{response.Error, response.Snapshot.Diagnostic} {
				if diagnostic == "" || len(diagnostic) > maxDiagnosticBytes || !utf8.ValidString(diagnostic) || !strings.HasPrefix(text, diagnostic) {
					t.Fatalf("diagnostic was not a bounded valid UTF-8 prefix: %q", diagnostic)
				}
			}
		})
	}
}

func TestRequestOnlyCrashRetryCompletesOrdinaryLock(t *testing.T) {
	request := minimalRequest(t)
	if _, _, err := publishOrAdmitRequest(request); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(request.RuntimeDirectory, lockName)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := publishOrAdmitRequest(request); err != nil {
		t.Fatal(err)
	}
	lock, err := acquireGenerationLock(request)
	if err != nil {
		t.Fatal(err)
	}
	lock.Close()
}

func TestOversizeLaunchRejectsBeforePublication(t *testing.T) {
	request := minimalRequest(t)
	request.HostConfigPath = "/" + strings.Repeat("\x01", 4000)
	if _, _, err := publishOrAdmitRequest(request); err == nil {
		t.Fatal("oversized encoded request was published")
	}
	if _, err := os.Lstat(request.RuntimeDirectory); !os.IsNotExist(err) {
		t.Fatal("oversized request mutated generation")
	}
}

func TestControlRejectsWrongBindingAndUnknownActionBeforeStop(t *testing.T) {
	for _, kind := range []string{"binding", "action", "version"} {
		t.Run(kind, func(t *testing.T) {
			binding := minimalRequest(t).Binding
			request := controlRequest{Version: 1, Binding: binding, Action: "stop"}
			switch kind {
			case "binding":
				request.Binding.Generation = "other"
			case "action":
				request.Action = "exec"
			case "version":
				request.Version = 2
			}
			server, client := net.Pipe()
			defer client.Close()
			owner := &runtimeFixture{done: make(chan struct{})}
			served := make(chan struct{})
			go func() {
				handleControl(context.Background(), server, binding, owner, func() error { return owner.Stop(context.Background()) })
				close(served)
			}()
			data, err := json.Marshal(request)
			if err != nil {
				t.Fatal(err)
			}
			client.SetDeadline(time.Now().Add(time.Second))
			if err := writeFrame(client, data); err != nil {
				t.Fatal(err)
			}
			if _, err := readBounded(client); err == nil {
				t.Fatal("invalid control request received response")
			}
			<-served
			if owner.stops.Load() != 0 {
				t.Fatal("invalid request reached stop")
			}
		})
	}
}

func TestClientRejectsForeignResponseAndHonorsCancellation(t *testing.T) {
	for _, kind := range []string{"outer-binding", "snapshot-binding", "oversize-diagnostic", "cancel"} {
		t.Run(kind, func(t *testing.T) {
			request := minimalRequest(t)
			if _, _, err := publishOrAdmitRequest(request); err != nil {
				t.Fatal(err)
			}
			lock, err := acquireGenerationLock(request)
			if err != nil {
				t.Fatal(err)
			}
			defer lock.Close()
			listener, err := listenSocket(filepath.Join(request.RuntimeDirectory, socketName))
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			accepted := make(chan struct{})
			done := make(chan struct{})
			release := make(chan struct{})
			defer close(release)
			go func() {
				defer close(done)
				connection, err := listener.AcceptUnix()
				if err != nil {
					return
				}
				defer connection.Close()
				if _, err := readBounded(connection); err != nil {
					return
				}
				close(accepted)
				if kind == "cancel" {
					<-release
					return
				}
				response := controlResponse{Version: 1, Binding: request.Binding, Snapshot: Snapshot{Binding: request.Binding, ObservedAt: time.Now().UTC()}}
				switch kind {
				case "outer-binding":
					response.Binding.Domain = "foreign"
				case "snapshot-binding":
					response.Snapshot.Binding.Generation = "foreign"
				case "oversize-diagnostic":
					response.Snapshot.Diagnostic = string(bytes.Repeat([]byte("x"), maxDiagnosticBytes+1))
				}
				data, _ := json.Marshal(response)
				writeFrame(connection, data)
			}()
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if kind == "cancel" {
				go func() { <-accepted; cancel() }()
			}
			client := &Client{RuntimeDirectory: request.RuntimeDirectory}
			if _, err := client.Snapshot(ctx, request.Binding); err == nil {
				t.Fatal("invalid response accepted")
			} else if kind == "cancel" && !errors.Is(err, context.Canceled) {
				t.Fatalf("cancellation: %v", err)
			}
			if kind != "cancel" {
				<-done
			}
		})
	}
}

func TestRunCancellationStopsAndReapsOnce(t *testing.T) {
	request := minimalRequest(t)
	path, _, err := publishOrAdmitRequest(request)
	if err != nil {
		t.Fatal(err)
	}
	owner := &runtimeFixture{done: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- Run(ctx, path, owner) }()
	client := &Client{RuntimeDirectory: request.RuntimeDirectory}
	if _, err := awaitSnapshot(context.Background(), request.Binding, startupPolicy{timeout: time.Second, interval: time.Millisecond}, client.Snapshot); err != nil {
		t.Fatal(err)
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("Run cancellation: %v", err)
	}
	if owner.stops.Load() != 1 || owner.waits.Load() != 1 {
		t.Fatalf("stop/wait = %d/%d", owner.stops.Load(), owner.waits.Load())
	}
}

type listenerFailureRuntime struct{ runtimeFixture }

func (o *listenerFailureRuntime) Start(ctx context.Context, request LaunchRequest) error {
	if err := o.runtimeFixture.Start(ctx, request); err != nil {
		return err
	}
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: filepath.Join(request.RuntimeDirectory, socketName), Net: "unix"})
	if err != nil {
		return err
	}
	listener.SetUnlinkOnClose(false)
	if err := os.Chmod(filepath.Join(request.RuntimeDirectory, socketName), 0600); err != nil {
		listener.Close()
		return err
	}
	return listener.Close()
}

// A listener failure cannot transfer socket-unlink authority to generic
// generation cleanup, even after the runtime has actually reaped.
func TestRunPreservesResidualSocketAfterListenerFailureAndActualReap(t *testing.T) {
	request := minimalRequest(t)
	path, _, err := publishOrAdmitRequest(request)
	if err != nil {
		t.Fatal(err)
	}
	owner := &listenerFailureRuntime{runtimeFixture: runtimeFixture{done: make(chan struct{})}}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := Run(context.Background(), path, owner); err == nil || !strings.Contains(err.Error(), "control socket already exists") {
		t.Fatalf("Run() error = %v, want listener failure", err)
	}
	if owner.starts.Load() != 1 || owner.stops.Load() != 1 || owner.waits.Load() != 1 {
		t.Fatalf("start/stop/wait = %d/%d/%d, want 1/1/1", owner.starts.Load(), owner.stops.Load(), owner.waits.Load())
	}
	assertPreservedSocketGeneration(t, request, before, nil)
}

// Catch Run discarding exact listener cleanup refusal and then unlinking a
// substituted 0600 socket through generic outer-generation cleanup.
func TestRunPreservesReplacementSocketAndEntireGenerationAfterCloseRefusal(t *testing.T) {
	request := minimalRequest(t)
	path, _, err := publishOrAdmitRequest(request)
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	owner := &runtimeFixture{done: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- Run(ctx, path, owner) }()
	client := &Client{RuntimeDirectory: request.RuntimeDirectory}
	if _, err := awaitSnapshot(ctx, request.Binding, startupPolicy{timeout: time.Second, interval: time.Millisecond}, client.Snapshot); err != nil {
		t.Fatal(err)
	}
	socketPath := filepath.Join(request.RuntimeDirectory, socketName)
	if err := os.Remove(socketPath); err != nil {
		t.Fatal(err)
	}
	replacement, err := net.ListenUnix("unix", &net.UnixAddr{Name: socketPath, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	replacement.SetUnlinkOnClose(false)
	defer replacement.Close()
	if err := os.Chmod(socketPath, 0600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(socketPath)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	err = <-done
	if !errors.Is(err, context.Canceled) || !strings.Contains(err.Error(), "control socket changed; refuse unlink") {
		t.Errorf("Run did not retain listener cleanup refusal: %v", err)
	}
	if owner.stops.Load() != 1 || owner.waits.Load() != 1 {
		t.Fatalf("stop/wait = %d/%d", owner.stops.Load(), owner.waits.Load())
	}
	assertPreservedSocketGeneration(t, request, before, info)
}

func assertPreservedSocketGeneration(t *testing.T, request LaunchRequest, requestBytes []byte, socketInfo os.FileInfo) {
	t.Helper()
	entries, err := os.ReadDir(request.RuntimeDirectory)
	if err != nil {
		t.Fatalf("unproven socket cleanup removed generation: %v", err)
	}
	if len(entries) != 3 || entries[0].Name() != lockName || entries[1].Name() != requestName || entries[2].Name() != socketName {
		t.Fatalf("partial generation cleanup: %v", entries)
	}
	after, err := os.ReadFile(filepath.Join(request.RuntimeDirectory, requestName))
	if err != nil || !bytes.Equal(after, requestBytes) {
		t.Fatalf("exact request changed: %q %v", after, err)
	}
	lockBytes, err := os.ReadFile(filepath.Join(request.RuntimeDirectory, lockName))
	if err != nil || len(lockBytes) != 0 {
		t.Fatalf("lock changed: %q %v", lockBytes, err)
	}
	info, err := os.Lstat(filepath.Join(request.RuntimeDirectory, socketName))
	if err != nil || info.Mode()&os.ModeSocket == 0 || info.Mode().Perm() != 0600 {
		t.Fatalf("residual socket changed: %v %v", info, err)
	}
	if socketInfo != nil && !os.SameFile(info, socketInfo) {
		t.Fatal("replacement socket inode changed")
	}
}

type startFailureRuntime struct {
	err   error
	start func(LaunchRequest) error
}

func (o *startFailureRuntime) Start(_ context.Context, request LaunchRequest) error {
	if o.start != nil {
		return o.start(request)
	}
	return o.err
}
func (*startFailureRuntime) Snapshot(context.Context) Snapshot { return Snapshot{} }
func (*startFailureRuntime) Stop(context.Context) error        { return errors.New("unexpected stop") }
func (*startFailureRuntime) Wait(context.Context) error        { return errors.New("unexpected wait") }

// Production break: retaining a failed generation after RuntimeOwner.Start has
// completed its partial cleanup would make the same durable retry ambiguous.
func TestRunRemovesCleanExactGenerationAfterOwnerStartFailure(t *testing.T) {
	request := minimalRequest(t)
	path, _, err := publishOrAdmitRequest(request)
	if err != nil {
		t.Fatal(err)
	}
	want := errors.New("owner start failed after cleanup")
	if err := Run(context.Background(), path, &startFailureRuntime{err: want}); !errors.Is(err, want) {
		t.Fatalf("Run() error = %v, want %v", err, want)
	}
	if _, err := os.Lstat(request.RuntimeDirectory); !os.IsNotExist(err) {
		t.Fatalf("failed generation retained: %v", err)
	}
}

// Production break: recursive cleanup could erase foreign or poisoned state
// merely because it appeared beneath a generation that otherwise matched.
func TestRunFailureCleanupRejectsUnexpectedContentsWithoutRecursing(t *testing.T) {
	request := minimalRequest(t)
	path, _, err := publishOrAdmitRequest(request)
	if err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(request.RuntimeDirectory, "foreign", "sentinel")
	owner := &startFailureRuntime{start: func(LaunchRequest) error {
		if err := os.Mkdir(filepath.Dir(sentinel), 0700); err != nil {
			return err
		}
		if err := os.WriteFile(sentinel, []byte("retain"), 0600); err != nil {
			return err
		}
		return errors.New("owner start failed")
	}}
	if err := Run(context.Background(), path, owner); err == nil || !strings.Contains(err.Error(), "unexpected generation entry") {
		t.Fatalf("Run() error = %v, want cleanup rejection", err)
	}
	if got, err := os.ReadFile(sentinel); err != nil || string(got) != "retain" {
		t.Fatalf("foreign sentinel = %q, %v, want retained", got, err)
	}
}

type slowStopRuntime struct{ runtimeFixture }

func (o *slowStopRuntime) Stop(ctx context.Context) error {
	o.stops.Add(1)
	<-ctx.Done()
	return ctx.Err()
}

func TestStopSharesOneLifecycleDeadlineAndRetainsOwnershipUntilReap(t *testing.T) {
	request := minimalRequest(t)
	path, _, err := publishOrAdmitRequest(request)
	if err != nil {
		t.Fatal(err)
	}
	owner := &slowStopRuntime{runtimeFixture: runtimeFixture{done: make(chan struct{})}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runDone := make(chan error, 1)
	go func() { runDone <- Run(ctx, path, owner) }()
	client := &Client{RuntimeDirectory: request.RuntimeDirectory}
	if _, err := awaitSnapshot(ctx, request.Binding, startupPolicy{timeout: time.Second, interval: time.Millisecond}, client.Snapshot); err != nil {
		select {
		case runErr := <-runDone:
			t.Fatalf("startup: %v; Run: %v", err, runErr)
		default:
			t.Fatal(err)
		}
	}
	err = client.Stop(context.Background(), request.Binding)
	if err == nil || !strings.Contains(err.Error(), "supervisor stop:") {
		t.Errorf("stop must return its bounded lifecycle result, not lose the response: %v", err)
	}
	if state, err := classifyExactGeneration(request); err != nil || state != exactGenerationLive {
		t.Errorf("lock released before reap: %v, %v", state, err)
	}
	close(owner.done)
	if err := <-runDone; !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("missing stop deadline result: %v", err)
	}
	if owner.stops.Load() != 1 || owner.waits.Load() != 1 {
		t.Fatalf("stop/wait = %d/%d", owner.stops.Load(), owner.waits.Load())
	}
}

type runtimeFixture struct {
	binding              Binding
	done                 chan struct{}
	once                 sync.Once
	starts, stops, waits atomic.Int32
}

func (o *runtimeFixture) Start(_ context.Context, r LaunchRequest) error {
	o.starts.Add(1)
	o.binding = r.Binding
	return nil
}
func (o *runtimeFixture) Snapshot(context.Context) Snapshot {
	return Snapshot{Binding: o.binding, BackendRunning: true, SerialHealthy: true, PinPresent: true, CertificateCurrent: true, ProbeOK: true, ZoneMatches: true}
}
func (o *runtimeFixture) Stop(context.Context) error {
	o.stops.Add(1)
	o.once.Do(func() { close(o.done) })
	return nil
}
func (o *runtimeFixture) Wait(context.Context) error { o.waits.Add(1); <-o.done; return nil }

type rejectLauncher struct{ calls atomic.Int32 }

func (l *rejectLauncher) Launch(context.Context, LaunchRequest) error {
	l.calls.Add(1)
	return errors.New("unexpected launch")
}

// Production break: trusting LaunchRequest.RuntimeDirectory independently of
// the configured root would let one binding target a foreign runtime tree.
func TestRootControllerRejectsRuntimeDirectoryOutsideItsRoot(t *testing.T) {
	request := minimalRequest(t)
	runtimeRoot := filepath.Dir(filepath.Dir(filepath.Dir(request.RuntimeDirectory)))
	foreign := minimalRequest(t)
	foreign.Binding = request.Binding
	launcher := &rejectLauncher{}
	control, err := NewExactController(runtimeRoot, launcher)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := control.StartExact(context.Background(), foreign); err == nil {
		t.Fatal("StartExact() accepted a caller-selected foreign runtime directory")
	}
	if launcher.calls.Load() != 0 {
		t.Fatal("foreign runtime directory reached launcher")
	}
	if _, err := os.Lstat(foreign.RuntimeDirectory); !os.IsNotExist(err) {
		t.Fatal("foreign runtime directory was mutated")
	}
}

func TestLiveReconnectStopAndSingleReap(t *testing.T) {
	request := minimalRequest(t)
	path, _, err := publishOrAdmitRequest(request)
	if err != nil {
		t.Fatal(err)
	}
	owner := &runtimeFixture{done: make(chan struct{})}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	runDone := make(chan error, 1)
	go func() { runDone <- Run(ctx, path, owner) }()
	client := &Client{RuntimeDirectory: request.RuntimeDirectory, MaxSnapshotAge: time.Minute}
	if _, err := awaitSnapshot(ctx, request.Binding, startupPolicy{timeout: time.Second, interval: time.Millisecond}, client.Snapshot); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{request.RuntimeDirectory, filepath.Join(request.RuntimeDirectory, socketName)} {
		info, err := os.Lstat(path)
		if err != nil {
			t.Fatal(err)
		}
		want := os.FileMode(0600)
		if info.IsDir() {
			want = 0700
		}
		if info.Mode().Perm() != want {
			t.Fatalf("mode %s", info.Mode())
		}
	}
	launcher := &rejectLauncher{}
	runtimeRoot := filepath.Dir(filepath.Dir(filepath.Dir(request.RuntimeDirectory)))
	control, err := NewExactController(runtimeRoot, launcher)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := control.StartExact(ctx, request); err != nil {
		t.Fatal(err)
	}
	if launcher.calls.Load() != 0 {
		t.Fatal("live generation relaunched")
	}
	foreign := request.Binding
	foreign.Generation = "foreign"
	if _, err := control.Snapshot(ctx, foreign); err == nil {
		t.Fatal("foreign binding admitted")
	}
	if err := control.Stop(ctx, request.Binding); err != nil {
		t.Fatal(err)
	}
	if err := <-runDone; err != nil {
		t.Fatal(err)
	}
	if owner.starts.Load() != 1 || owner.stops.Load() != 1 || owner.waits.Load() != 1 {
		t.Fatalf("start/stop/wait = %d/%d/%d", owner.starts.Load(), owner.stops.Load(), owner.waits.Load())
	}
	if _, err := os.Lstat(request.RuntimeDirectory); !os.IsNotExist(err) {
		t.Fatalf("generation retained after actual reap: %v", err)
	}
}

func TestPrivateSocketAdmission(t *testing.T) {
	for _, kind := range []string{"regular", "symlink", "public-socket", "public-parent"} {
		t.Run(kind, func(t *testing.T) {
			request := minimalRequest(t)
			if _, _, err := publishOrAdmitRequest(request); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(request.RuntimeDirectory, socketName)
			switch kind {
			case "regular":
				os.WriteFile(path, nil, 0600)
			case "symlink":
				os.Symlink(filepath.Join(request.RuntimeDirectory, requestName), path)
			default:
				listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
				if err != nil {
					t.Fatal(err)
				}
				defer listener.Close()
				os.Chmod(path, 0600)
				if kind == "public-socket" {
					os.Chmod(path, 0666)
				} else {
					os.Chmod(request.RuntimeDirectory, 0755)
				}
			}
			client := &Client{RuntimeDirectory: request.RuntimeDirectory}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if _, err := client.Snapshot(ctx, request.Binding); err == nil {
				t.Fatal("unsafe socket admitted")
			}
		})
	}
}

func TestGenerationRejectsAmbiguousState(t *testing.T) {
	for _, kind := range []string{"empty", "foreign", "malformed", "symlink-request", "symlink-directory", "unexpected", "lock-content", "lock-symlink", "stale-serial", "public-directory"} {
		t.Run(kind, func(t *testing.T) {
			request := minimalRequest(t)
			if _, _, err := publishOrAdmitRequest(request); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(request.RuntimeDirectory, requestName)
			switch kind {
			case "empty":
				os.Remove(path)
				os.Remove(filepath.Join(request.RuntimeDirectory, lockName))
			case "foreign":
				foreign := request
				foreign.Binding.SessionID = "other"
				data, _ := json.Marshal(foreign)
				os.WriteFile(path, data, 0600)
			case "malformed":
				os.WriteFile(path, []byte("{"), 0600)
			case "symlink-request":
				os.Rename(path, path+".saved")
				os.Symlink(path+".saved", path)
			case "symlink-directory":
				os.Rename(request.RuntimeDirectory, request.RuntimeDirectory+".saved")
				os.Symlink(request.RuntimeDirectory+".saved", request.RuntimeDirectory)
			case "unexpected":
				os.WriteFile(filepath.Join(request.RuntimeDirectory, "unknown"), nil, 0600)
			case "lock-content":
				os.WriteFile(filepath.Join(request.RuntimeDirectory, lockName), []byte("foreign"), 0600)
			case "lock-symlink":
				os.Remove(filepath.Join(request.RuntimeDirectory, lockName))
				os.Symlink(path, filepath.Join(request.RuntimeDirectory, lockName))
			case "stale-serial":
				os.Mkdir(filepath.Join(request.RuntimeDirectory, "serial"), 0700)
			case "public-directory":
				os.Chmod(request.RuntimeDirectory, 0755)
			}
			if _, err := classifyExactGeneration(request); err == nil {
				t.Fatal("ambiguous generation admitted")
			}
			if _, _, err := publishOrAdmitRequest(request); err == nil {
				t.Fatal("ambiguous generation reused")
			}
		})
	}
}

func TestMinimalRequestPublishesAndRetriesWithoutHostEvidence(t *testing.T) {
	request := minimalRequest(t)
	path, first, err := publishOrAdmitRequest(request)
	if err != nil {
		t.Fatalf("minimal expected binding must publish: %v", err)
	}
	if !first {
		t.Fatal("first publication not reported")
	}
	again, first, err := publishOrAdmitRequest(request)
	if err != nil || first || again != path {
		t.Fatalf("exact retry = %q, %v, %v", again, first, err)
	}
	if err := RunRequest(context.Background(), path); err == nil {
		t.Fatal("uncomposed runtime reported success")
	}
}
