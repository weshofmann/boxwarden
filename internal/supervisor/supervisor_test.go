package supervisor

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestSupervisorLaunchPersistsNoBarePIDOwnership(t *testing.T) {
	dir := privateRuntime(t)
	binding := testBinding()
	identity := ProcessIdentity{PID: os.Getpid(), StartedAt: time.Unix(100, 0).UTC(), Unique: 7}
	owner := newTestOwner(identity)
	request := LaunchRequest{Binding: binding, RuntimeDirectory: dir, HostConfigPath: "/private/config"}
	if err := writeLaunchRequest(filepath.Join(dir, requestName), request); err != nil {
		t.Fatal(err)
	}
	runtimeIdentity, err := capturePrivateDirectory(dir)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := prepare(context.Background(), filepath.Join(dir, requestName), owner, testInspector(identity), runtimeIdentity)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Evidence.Supervisor.PID != identity.PID || manifest.Evidence.Supervisor.Unique == 0 || manifest.Evidence.Supervisor.StartedAt.IsZero() {
		t.Fatalf("manifest supervisor evidence = %#v, want full kernel identity", manifest.Evidence.Supervisor)
	}
	if len(manifest.Evidence.Children) != 2 || manifest.Evidence.Children[0].Identity.Unique == 0 || len(manifest.Evidence.Endpoints) != 2 {
		t.Fatalf("manifest runtime evidence = %#v, want complete named evidence", manifest.Evidence)
	}
	for _, path := range []string{filepath.Join(dir, requestName), filepath.Join(dir, manifestName)} {
		info, err := os.Lstat(path)
		if err != nil {
			t.Fatal(err)
		}
		if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
			t.Fatalf("%s mode=%#o, want private regular 0600", path, info.Mode())
		}
	}
}

func TestControlRejectsWrongBindingChallengeOrMAC(t *testing.T) {
	service, controller, cancel := runningService(t, false)
	defer cancel()
	if _, err := controller.call(context.Background(), testBinding(), "snapshot", "different-challenge", []byte("wrong")); err == nil {
		t.Fatal("wrong MAC accepted")
	}
	wrong := testBinding()
	wrong.Generation = "other-generation"
	if _, err := controller.call(context.Background(), wrong, "snapshot", randomChallenge(t), service.key); err == nil {
		t.Fatal("wrong binding accepted")
	}
	response, err := controller.call(context.Background(), testBinding(), "snapshot", randomChallenge(t), service.key)
	if err != nil {
		t.Fatal(err)
	}
	if response.Challenge == "" || response.Binding != testBinding() {
		t.Fatalf("response = %#v, want bound challenge response", response)
	}
}

func TestControllerRejectsPIDReuseAndStaleManifest(t *testing.T) {
	service, controller, cancel := runningService(t, false)
	defer cancel()
	service.inspector.set(service.identity.PID, ProcessIdentity{PID: service.identity.PID, StartedAt: service.identity.StartedAt.Add(time.Second), Unique: service.identity.Unique + 1})
	if _, err := controller.Snapshot(context.Background(), testBinding()); err == nil {
		t.Fatal("PID reuse accepted")
	}
	service.inspector.set(service.identity.PID, service.identity)
	manifestPath := filepath.Join(service.dir, manifestName)
	stale, err := readManifest(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	stale.Binding.Generation = "other-generation"
	if err := os.Remove(manifestPath); err != nil {
		t.Fatal(err)
	}
	if err := writeManifest(manifestPath, stale); err != nil {
		t.Fatal(err)
	}
	if _, err := controller.Snapshot(context.Background(), testBinding()); err == nil {
		t.Fatal("stale replacement manifest accepted")
	}
}

func TestSupervisorReapsDirectChildrenOnBackendExit(t *testing.T) {
	service, _, cancel := runningService(t, false)
	defer cancel()
	service.owner.exit(nil)
	select {
	case <-service.done:
	case <-time.After(time.Second):
		t.Fatal("supervisor did not reap after backend exit")
	}
	if got := service.owner.stopCalls(); got != 1 {
		t.Fatalf("owned stop calls=%d, want 1", got)
	}
	if got := service.owner.closeCalls(); got != 1 {
		t.Fatalf("owned close calls=%d, want 1", got)
	}
	if _, err := os.Lstat(filepath.Join(service.dir, manifestName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("manifest after cleanup = %v, want absent", err)
	}
}

func TestSnapshotIsBoundedAndCannotReportReadyAfterBrokerPoison(t *testing.T) {
	service, controller, cancel := runningService(t, true)
	defer cancel()
	snapshot, err := controller.Snapshot(context.Background(), testBinding())
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Diagnostic) > maxDiagnosticBytes || snapshotReady(snapshot) {
		t.Fatalf("snapshot = %#v, want bounded non-ready poison state", snapshot)
	}
	service.owner.setHealthy(true)
	stillPoisoned, err := controller.Snapshot(context.Background(), testBinding())
	if err != nil || snapshotReady(stillPoisoned) {
		t.Fatalf("poisoned runtime recovered through snapshot: %#v, %v", stillPoisoned, err)
	}
}

func TestPrepareRejectsUnsupportedPlatformBeforeRuntimeStart(t *testing.T) {
	dir := privateRuntime(t)
	requestPath := filepath.Join(dir, requestName)
	if err := writeLaunchRequest(requestPath, LaunchRequest{Binding: testBinding(), RuntimeDirectory: dir, HostConfigPath: "/private/config"}); err != nil {
		t.Fatal(err)
	}
	owner := newTestOwner(ProcessIdentity{PID: 1, StartedAt: time.Unix(1, 0), Unique: 1})
	runtimeIdentity, err := capturePrivateDirectory(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := prepare(context.Background(), requestPath, owner, unsupportedInspector{}, runtimeIdentity); err == nil {
		t.Fatal("unsupported platform prepared a runtime")
	}
	if owner.started() {
		t.Fatal("unsupported platform started runtime")
	}
	if _, err := os.Lstat(filepath.Join(dir, manifestName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("manifest=%v, want absent", err)
	}
}

func TestDetachedLauncherUsesFixedInternalArgvAndClosedEnvironment(t *testing.T) {
	dir := privateRuntime(t)
	identity := ProcessIdentity{PID: os.Getpid(), StartedAt: time.Unix(300, 0), Unique: 30}
	var got LaunchCommand
	child := &testLaunchChild{}
	launcher := newDetachedLauncher(launcherDeps{executable: func() (string, error) { return "/private/boxwarden", nil }, inspector: testInspector(identity), start: func(_ context.Context, command LaunchCommand) (launchChild, error) {
		got = command
		return child, nil
	}, await: func(context.Context, *Client, Binding) error { return nil }})
	request := LaunchRequest{Binding: testBinding(), RuntimeDirectory: dir, HostConfigPath: "/private/config"}
	if err := launcher.Launch(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if got.Path != "/private/boxwarden" || got.Dir != dir || len(got.Args) != 3 || got.Args[0] != "internal" || got.Args[1] != "session-supervisor" || got.Args[2] != filepath.Join(dir, requestName) {
		t.Fatalf("launch command=%#v", got)
	}
	if len(got.Env) != 3 || got.Env[0] != "PATH=/usr/bin:/bin" || !child.released {
		t.Fatalf("launch env=%#v", got.Env)
	}
}

type testLaunchChild struct{ released, stopped bool }

func (c *testLaunchChild) release() error                 { c.released = true; return nil }
func (c *testLaunchChild) stopReap(context.Context) error { c.stopped = true; return nil }

func TestControlFramesSplitReadsAndReturnsStopFailure(t *testing.T) {
	owner := newTestOwner(ProcessIdentity{PID: 1, StartedAt: time.Unix(1, 0), Unique: 1})
	owner.stopFailure = errors.New("stop refused")
	key := make([]byte, 32)
	key[0] = 1
	manifest := Manifest{Binding: testBinding()}
	client, server := net.Pipe()
	defer client.Close()
	go handleControl(server, manifest, key, owner, func() error { return owner.Stop(context.Background()) })
	request := controlRequest{Version: 1, Action: "stop", Binding: testBinding(), Challenge: "fresh"}
	var err error
	request.MAC, err = requestMAC(key, request)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	frame := append([]byte{0, 0, 0, byte(len(data))}, data...)
	if _, err := client.Write(frame[:2]); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Write(frame[2:]); err != nil {
		t.Fatal(err)
	}
	responseData, err := readBounded(client)
	if err != nil {
		t.Fatal(err)
	}
	var response controlResponse
	if err := decodeExact(responseData, &response); err != nil {
		t.Fatal(err)
	}
	if response.Error != "stop refused" || owner.stopCalls() != 1 {
		t.Fatalf("stop response=%#v calls=%d", response, owner.stopCalls())
	}
}

func TestCleanupPreservesSameModeManifestReplacement(t *testing.T) {
	service, _, cancel := runningService(t, false)
	manifestPath := filepath.Join(service.dir, manifestName)
	if err := os.Remove(manifestPath); err != nil {
		t.Fatal(err)
	}
	if err := writePrivateFile(manifestPath, []byte("replacement")); err != nil {
		t.Fatal(err)
	}
	cancel()
	select {
	case <-service.done:
	case <-time.After(time.Second):
		t.Fatal("supervisor did not finish cleanup")
	}
	if _, err := os.Lstat(manifestPath); err != nil {
		t.Fatalf("same-mode replacement was removed: %v", err)
	}
}

func TestSuccessfulStopWaitsForHeldOwnerBeforeClosingNamespace(t *testing.T) {
	service, controller, cancel := runningService(t, false)
	defer cancel()
	if err := controller.Stop(context.Background(), testBinding()); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(100 * time.Millisecond)
	for time.Now().Before(deadline) {
		if service.owner.closeCalls() != 0 {
			t.Fatal("supervisor closed runtime before held owner was reaped")
		}
		time.Sleep(time.Millisecond)
	}
	service.owner.exit(nil)
	select {
	case <-service.done:
	case <-time.After(time.Second):
		t.Fatal("supervisor did not close after owner exit")
	}
}

func TestDetachedLauncherReapsOnlyUnauthenticatedChildAndOwnRequest(t *testing.T) {
	dir := privateRuntime(t)
	identity := ProcessIdentity{PID: os.Getpid(), StartedAt: time.Unix(400, 0).UTC(), Unique: 40}
	child := &testLaunchChild{}
	launcher := newDetachedLauncher(launcherDeps{
		executable: func() (string, error) { return "/private/boxwarden", nil },
		inspector:  testInspector(identity),
		start:      func(context.Context, LaunchCommand) (launchChild, error) { return child, nil },
		await:      func(context.Context, *Client, Binding) error { return errors.New("no authenticated evidence") },
	})
	err := launcher.Launch(context.Background(), LaunchRequest{Binding: testBinding(), RuntimeDirectory: dir, HostConfigPath: "/private/config"})
	if err == nil || !child.stopped || child.released {
		t.Fatalf("failed launch = %v, stopped=%t released=%t; want reaped but not released", err, child.stopped, child.released)
	}
	if _, err := os.Lstat(filepath.Join(dir, requestName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed launch request remains: %v", err)
	}
}

func TestDetachedLauncherRefusesUnsupportedPlatformBeforeRequestMutation(t *testing.T) {
	dir := privateRuntime(t)
	launcher := newDetachedLauncher(launcherDeps{inspector: unsupportedInspector{}})
	err := launcher.Launch(context.Background(), LaunchRequest{Binding: testBinding(), RuntimeDirectory: dir, HostConfigPath: "/private/config"})
	if err == nil {
		t.Fatal("unsupported platform launched")
	}
	if _, err := os.Lstat(filepath.Join(dir, requestName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unsupported platform wrote request: %v", err)
	}
}

func TestGenerationLockRejectsSameUIDSymlinkAndContention(t *testing.T) {
	dir := privateRuntime(t)
	target := filepath.Join(dir, "same-uid-target")
	if err := writePrivateFile(target, []byte("target")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(dir, lockName)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := acquireGenerationLock(dir); err == nil {
		t.Fatal("same-UID 0600 lock symlink was admitted")
	}
	if err := os.Remove(filepath.Join(dir, lockName)); err != nil {
		t.Fatal(err)
	}
	first, _, err := acquireGenerationLock(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	if _, _, err := acquireGenerationLock(dir); err == nil {
		t.Fatal("second lock owner was admitted")
	}
}

func TestRuntimeEvidenceRequiresEachNamedRoleAndOneBrokerState(t *testing.T) {
	identity := ProcessIdentity{PID: 1, StartedAt: time.Unix(1, 0).UTC(), Unique: 1}
	if err := validStartEvidence(RuntimeStartEvidence{Children: []NamedProcessEvidence{{Role: "backend", Identity: identity}, {Role: "backend", Identity: identity}}, Endpoints: []NamedFileEvidence{{Role: "tart-endpoint", Identity: FileIdentity{Path: "/private/tart", Device: 1, Inode: 1}}, {Role: "operator-endpoint", Identity: FileIdentity{Path: "/private/operator", Device: 1, Inode: 2}}}, Broker: BrokerEvidence{Healthy: true}}); err == nil {
		t.Fatal("duplicate direct-child role was admitted")
	}
	if err := validStartEvidence(RuntimeStartEvidence{Children: []NamedProcessEvidence{{Role: "backend", Identity: identity}, {Role: "screen", Identity: identity}}, Endpoints: []NamedFileEvidence{{Role: "tart-endpoint", Identity: FileIdentity{Path: "/private/tart", Device: 1, Inode: 1}}, {Role: "operator-endpoint", Identity: FileIdentity{Path: "/private/operator", Device: 1, Inode: 2}}}, Broker: BrokerEvidence{}}); err == nil {
		t.Fatal("unknown initial broker state was admitted")
	}
}

func TestControlHandlerAuthenticatesDirectFramesAndRejectsUnknownActions(t *testing.T) {
	key := make([]byte, 32)
	key[0] = 9
	owner := newTestOwner(ProcessIdentity{PID: 1, StartedAt: time.Unix(1, 0).UTC(), Unique: 1})
	manifest := Manifest{Binding: testBinding()}
	valid := controlRequest{Version: 1, Action: "snapshot", Binding: testBinding(), Challenge: "fresh"}
	var err error
	valid.MAC, err = requestMAC(key, valid)
	if err != nil {
		t.Fatal(err)
	}
	for name, request := range map[string]controlRequest{
		"wrong-binding":  func() controlRequest { r := valid; r.Binding.Generation = "other"; return r }(),
		"wrong-mac":      func() controlRequest { r := valid; r.MAC = "wrong"; return r }(),
		"unknown-action": func() controlRequest { r := valid; r.Action = "repair"; r.MAC, _ = requestMAC(key, r); return r }(),
	} {
		t.Run(name, func(t *testing.T) {
			client, server := net.Pipe()
			defer client.Close()
			go handleControl(server, manifest, key, owner, func() error { return nil })
			data, err := json.Marshal(request)
			if err != nil {
				t.Fatal(err)
			}
			if err := writeFrame(client, data); err != nil {
				t.Fatal(err)
			}
			_ = client.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
			if _, err := readBounded(client); err == nil {
				t.Fatal("unauthenticated or unknown action received a response")
			}
		})
	}
	client, server := net.Pipe()
	defer client.Close()
	go handleControl(server, manifest, key, owner, func() error { return nil })
	data, err := json.Marshal(valid)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeFrame(client, data); err != nil {
		t.Fatal(err)
	}
	responseData, err := readBounded(client)
	if err != nil {
		t.Fatal(err)
	}
	var response controlResponse
	if err := decodeExact(responseData, &response); err != nil {
		t.Fatal(err)
	}
	want, err := responseMAC(key, response)
	if err != nil || response.Challenge != "fresh" || response.MAC != want {
		t.Fatalf("response was not challenge/MAC bound: %#v", response)
	}
}

func TestWriteFrameCompletesShortWrites(t *testing.T) {
	writer := &shortFrameWriter{limit: 2}
	if err := writeFrame(writer, []byte("frame-body")); err != nil {
		t.Fatal(err)
	}
	if got, want := binary.BigEndian.Uint32(writer.data[:4]), uint32(len("frame-body")); got != want {
		t.Fatalf("frame size=%d, want=%d", got, want)
	}
	if got := string(writer.data[4:]); got != "frame-body" {
		t.Fatalf("frame body=%q", got)
	}
}

type shortFrameWriter struct {
	limit int
	data  []byte
}

func (w *shortFrameWriter) Write(data []byte) (int, error) {
	if len(data) > w.limit {
		data = data[:w.limit]
	}
	w.data = append(w.data, data...)
	return len(data), nil
}

type testService struct {
	dir       string
	key       []byte
	identity  ProcessIdentity
	owner     *testOwner
	inspector *mutableInspector
	done      <-chan struct{}
}

func runningService(t *testing.T, poisoned bool) (testService, *Client, context.CancelFunc) {
	t.Helper()
	dir := privateRuntime(t)
	identity := ProcessIdentity{PID: os.Getpid(), StartedAt: time.Unix(200, 0).UTC(), Unique: 9}
	owner := newTestOwner(identity)
	owner.poisoned = poisoned
	requestPath := filepath.Join(dir, requestName)
	if err := writeLaunchRequest(requestPath, LaunchRequest{Binding: testBinding(), RuntimeDirectory: dir, HostConfigPath: "/private/config"}); err != nil {
		t.Fatal(err)
	}
	inspector := testInspector(identity)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	runErr := make(chan error, 1)
	go func() { defer close(done); runErr <- Run(ctx, requestPath, owner, inspector) }()
	client := &Client{RuntimeDirectory: dir, Inspector: inspector, MaxSnapshotAge: time.Minute}
	var manifest Manifest
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		var err error
		manifest, err = readManifest(filepath.Join(dir, manifestName))
		if err == nil && socketIsPrivate(filepath.Join(dir, socketName)) {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if manifest.ControlKey == "" || !socketIsPrivate(filepath.Join(dir, socketName)) {
		info, statErr := os.Lstat(filepath.Join(dir, socketName))
		cancel()
		select {
		case err := <-runErr:
			if err != nil && strings.Contains(err.Error(), "operation not permitted") {
				t.Skipf("sandbox denies owner-private Unix socket: %v", err)
			}
			t.Fatalf("supervisor did not publish manifest: %v (socket=%#v stat=%v)", err, info, statErr)
		default:
			t.Fatalf("supervisor did not publish manifest (socket=%#v stat=%v)", info, statErr)
		}
	}
	key, err := decodeKey(manifest.ControlKey)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	return testService{dir: dir, key: key, identity: identity, owner: owner, inspector: inspector, done: done}, client, cancel
}
func socketIsPrivate(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.Mode()&os.ModeSocket != 0 && info.Mode().Perm() == 0o600
}
func testBinding() Binding {
	return Binding{Domain: "work", SessionID: "session-uuid", BackendKind: "fake", BackendObject: "object-1", Generation: "generation-1"}
}
func privateRuntime(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/private/tmp", "bw-sup-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	return dir
}
func randomChallenge(t *testing.T) string {
	t.Helper()
	value, err := newChallenge()
	if err != nil {
		t.Fatal(err)
	}
	return value
}

type mutableInspector struct {
	mu      sync.Mutex
	entries map[int]ProcessIdentity
}

func testInspector(identity ProcessIdentity) *mutableInspector {
	return &mutableInspector{entries: map[int]ProcessIdentity{identity.PID: identity, 72: {PID: 72, StartedAt: time.Unix(201, 0).UTC(), Unique: 10}, 73: {PID: 73, StartedAt: time.Unix(202, 0).UTC(), Unique: 11}}}
}
func (i *mutableInspector) Supported() bool { return true }
func (i *mutableInspector) Observe(_ context.Context, pid int) (ProcessIdentity, error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	v, ok := i.entries[pid]
	if !ok {
		return ProcessIdentity{}, errors.New("missing process")
	}
	return v, nil
}
func (i *mutableInspector) set(pid int, identity ProcessIdentity) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.entries[pid] = identity
}

type unsupportedInspector struct{}

func (unsupportedInspector) Supported() bool { return false }
func (unsupportedInspector) Observe(context.Context, int) (ProcessIdentity, error) {
	return ProcessIdentity{}, errors.New("unsupported")
}

type testOwner struct {
	mu                          sync.Mutex
	identity                    ProcessIdentity
	poisoned, healthy, didStart bool
	exitCh                      chan error
	stopped, closed             int
	stopFailure                 error
	endpoints                   []string
}

func newTestOwner(identity ProcessIdentity) *testOwner {
	return &testOwner{identity: identity, healthy: true, exitCh: make(chan error, 1)}
}
func (o *testOwner) Start(_ context.Context, request LaunchRequest) (RuntimeStartEvidence, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.didStart = true
	tartPath := filepath.Join(request.RuntimeDirectory, "test-tart-endpoint")
	operatorPath := filepath.Join(request.RuntimeDirectory, "test-operator-endpoint")
	if err := writePrivateFile(tartPath, []byte("tart")); err != nil {
		return RuntimeStartEvidence{}, err
	}
	if err := writePrivateFile(operatorPath, []byte("operator")); err != nil {
		return RuntimeStartEvidence{}, err
	}
	tart, err := capturePrivateRegular(tartPath)
	if err != nil {
		return RuntimeStartEvidence{}, err
	}
	operator, err := capturePrivateRegular(operatorPath)
	if err != nil {
		return RuntimeStartEvidence{}, err
	}
	o.endpoints = []string{tartPath, operatorPath}
	return RuntimeStartEvidence{Children: []NamedProcessEvidence{{Role: "backend", Identity: ProcessIdentity{PID: 72, StartedAt: time.Unix(201, 0).UTC(), Unique: 10}}, {Role: "screen", Identity: ProcessIdentity{PID: 73, StartedAt: time.Unix(202, 0).UTC(), Unique: 11}}}, Endpoints: []NamedFileEvidence{{Role: "tart-endpoint", Identity: tart}, {Role: "operator-endpoint", Identity: operator}}, Broker: BrokerEvidence{Healthy: true}}, nil
}
func (o *testOwner) Snapshot() Snapshot {
	o.mu.Lock()
	defer o.mu.Unlock()
	return Snapshot{BackendRunning: o.healthy, BrokerHealthy: o.healthy && !o.poisoned, ScreenHealthy: o.healthy, PinPresent: true, CertificateCurrent: true, ProbeOK: true, ZoneMatches: true, Diagnostic: string(make([]byte, maxDiagnosticBytes+10))}
}
func (o *testOwner) Wait(context.Context) error { return <-o.exitCh }
func (o *testOwner) Stop(context.Context) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.stopped++
	return o.stopFailure
}
func (o *testOwner) Close(context.Context) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.closed++
	for _, path := range o.endpoints {
		_ = os.Remove(path)
	}
	return nil
}
func (o *testOwner) exit(err error)    { o.exitCh <- err }
func (o *testOwner) stopCalls() int    { o.mu.Lock(); defer o.mu.Unlock(); return o.stopped }
func (o *testOwner) closeCalls() int   { o.mu.Lock(); defer o.mu.Unlock(); return o.closed }
func (o *testOwner) setHealthy(v bool) { o.mu.Lock(); defer o.mu.Unlock(); o.healthy = v }
func (o *testOwner) started() bool     { o.mu.Lock(); defer o.mu.Unlock(); return o.didStart }
