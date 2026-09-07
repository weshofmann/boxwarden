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
	"syscall"
	"testing"
	"time"

	"github.com/weshofmann/boxwarden/internal/hostx"
)

func TestSupervisorLaunchPersistsNoBarePIDOwnership(t *testing.T) {
	dir := privateRuntime(t)
	binding := testBinding()
	identity := ProcessIdentity{PID: os.Getpid(), StartedAt: time.Unix(100, 0).UTC(), Unique: 7}
	owner := newTestOwner(identity)
	request := LaunchRequest{Binding: binding, RuntimeDirectory: dir, HostConfigPath: "/private/config", SessionRecordName: "dev", Host: testHostExpectation(), CA: testCAExpectation()}
	if err := writeLaunchRequest(filepath.Join(dir, requestName), request); err != nil {
		t.Fatal(err)
	}
	runtimeIdentity, err := capturePrivateDirectory(dir)
	if err != nil {
		t.Fatal(err)
	}
	manifest, _, err := prepare(context.Background(), request, owner, testInspector(identity), runtimeIdentity)
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

func TestManifestRejectsPathOutsideDeclaredRuntimeDirectory(t *testing.T) {
	dir := privateRuntime(t)
	other := privateRuntime(t)
	identity := ProcessIdentity{PID: os.Getpid(), StartedAt: time.Unix(101, 0).UTC(), Unique: 8}
	owner := newTestOwner(identity)
	requestPath := filepath.Join(dir, requestName)
	if err := writeLaunchRequest(requestPath, LaunchRequest{Binding: testBinding(), RuntimeDirectory: dir, HostConfigPath: "/private/config", SessionRecordName: "dev", Host: testHostExpectation(), CA: testCAExpectation()}); err != nil {
		t.Fatal(err)
	}
	runtimeIdentity, err := capturePrivateDirectory(dir)
	if err != nil {
		t.Fatal(err)
	}
	request, err := readLaunchRequest(requestPath)
	if err != nil {
		t.Fatal(err)
	}
	manifest, _, err := prepare(context.Background(), request, owner, testInspector(identity), runtimeIdentity)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(other, manifestName)
	if err := writeManifest(path, manifest); err == nil {
		t.Fatal("writeManifest accepted a manifest outside its declared runtime directory")
	}
	raw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := writePrivateFile(path, raw); err != nil {
		t.Fatal(err)
	}
	if _, err := readManifest(path); err == nil {
		t.Fatal("readManifest accepted a manifest outside its declared runtime directory")
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

func TestBackendExitUsesOneStoredReaperResult(t *testing.T) {
	service, _, cancel := runningService(t, false)
	defer cancel()
	started := time.Now()
	service.owner.exit(nil)
	select {
	case <-service.done:
		if elapsed := time.Since(started); elapsed >= lifecycleDeadline() {
			t.Fatalf("backend-exit cleanup took %v, indicating a second wait", elapsed)
		}
	case <-time.After(lifecycleDeadline()):
		t.Fatal("backend-exit cleanup waited for a second reaper result")
	}
}

func TestBackendExitReturnsTerminalWaitErrorOnce(t *testing.T) {
	service, _, cancel := runningService(t, false)
	defer cancel()
	waitErr := errors.New("backend terminal wait failure")
	service.owner.exit(waitErr)
	select {
	case err := <-service.result:
		if err == nil || strings.Count(err.Error(), waitErr.Error()) != 1 {
			t.Fatalf("Run() error = %v, want one terminal Wait result", err)
		}
	case <-time.After(time.Second):
		t.Fatal("backend-exit cleanup did not return")
	}
	if service.owner.stopCalls() != 1 || service.owner.closeCalls() != 1 {
		t.Fatalf("Stop=%d Close=%d, want one each", service.owner.stopCalls(), service.owner.closeCalls())
	}
	if _, err := os.Lstat(filepath.Join(service.dir, manifestName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("manifest after terminal Wait result = %v, want absent", err)
	}
}

func TestWaitTimeoutPreservesRuntimeNamespace(t *testing.T) {
	setLifecycleDeadline(t, 25*time.Millisecond)
	service, _, cancel := runningService(t, false)
	cancel()
	select {
	case <-service.done:
		t.Fatal("wait timeout abandoned the held generation")
	case <-time.After(2 * lifecycleDeadline()):
	}
	if service.owner.closeCalls() != 0 {
		t.Fatal("wait timeout closed a potentially live runtime")
	}
	if _, err := os.Lstat(filepath.Join(service.dir, manifestName)); err != nil {
		t.Fatalf("wait timeout removed ownership evidence: %v", err)
	}
	service.owner.exit(nil)
	select {
	case <-service.done:
	case <-time.After(time.Second):
		t.Fatal("supervisor did not complete ordered cleanup after the held reaper finished")
	}
	if service.owner.closeCalls() != 1 {
		t.Fatalf("owner close calls = %d, want one after reaping", service.owner.closeCalls())
	}
}

func TestSetupFailureAfterStartRetainsOwnershipUntilReaped(t *testing.T) {
	setLifecycleDeadline(t, 25*time.Millisecond)
	dir := privateRuntime(t)
	requestPath := filepath.Join(dir, requestName)
	if err := writeLaunchRequest(requestPath, LaunchRequest{Binding: testBinding(), RuntimeDirectory: dir, HostConfigPath: "/private/config", SessionRecordName: "dev", Host: testHostExpectation(), CA: testCAExpectation()}); err != nil {
		t.Fatal(err)
	}
	if err := writeBoundGenerationLock(filepath.Join(dir, lockName), LaunchRequest{Binding: testBinding(), RuntimeDirectory: dir, HostConfigPath: "/private/config", SessionRecordName: "dev", Host: testHostExpectation(), CA: testCAExpectation()}); err != nil {
		t.Fatal(err)
	}
	identity := ProcessIdentity{PID: os.Getpid(), StartedAt: time.Unix(600, 0).UTC(), Unique: 60}
	base := newTestOwner(identity)
	owner := invalidEvidenceOwner{testOwner: base}
	done := make(chan error, 1)
	go func() { done <- Run(context.Background(), requestPath, owner, testInspector(identity)) }()
	select {
	case err := <-done:
		t.Fatalf("setup failure abandoned a live owner: %v", err)
	case <-time.After(2 * lifecycleDeadline()):
	}
	if base.closeCalls() != 0 {
		t.Fatal("setup failure closed before reaping")
	}
	if _, err := os.Lstat(requestPath); err != nil {
		t.Fatalf("setup rollback removed request evidence before reaping: %v", err)
	}
	base.exit(nil)
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("setup failure was lost after cleanup")
		}
	case <-time.After(time.Second):
		t.Fatal("setup rollback did not finish after reaping")
	}
}

func TestRunCleansUpUnownedStartFailureWithoutStoppingOwner(t *testing.T) {
	dir := privateRuntime(t)
	requestPath := filepath.Join(dir, requestName)
	if err := writeLaunchRequest(requestPath, LaunchRequest{Binding: testBinding(), RuntimeDirectory: dir, HostConfigPath: "/private/config", SessionRecordName: "dev", Host: testHostExpectation(), CA: testCAExpectation()}); err != nil {
		t.Fatal(err)
	}
	if err := writeBoundGenerationLock(filepath.Join(dir, lockName), LaunchRequest{Binding: testBinding(), RuntimeDirectory: dir, HostConfigPath: "/private/config", SessionRecordName: "dev", Host: testHostExpectation(), CA: testCAExpectation()}); err != nil {
		t.Fatal(err)
	}
	identity := ProcessIdentity{PID: os.Getpid(), StartedAt: time.Unix(601, 0).UTC(), Unique: 61}
	owner := startResultOwner{testOwner: newTestOwner(identity), result: RuntimeStartResult{}, err: errors.New("start performed no mutation")}
	err := Run(context.Background(), requestPath, owner, testInspector(identity))
	if err == nil || !strings.Contains(err.Error(), "start performed no mutation") {
		t.Fatalf("Run() error = %v, want unowned start failure", err)
	}
	if owner.stopCalls() != 0 || owner.closeCalls() != 0 {
		t.Fatalf("unowned start called Stop=%d Close=%d", owner.stopCalls(), owner.closeCalls())
	}
	if _, statErr := os.Lstat(requestPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("unowned start left request namespace: %v", statErr)
	}
}

func TestRunReapsPartialStartFailureBeforeClosingNamespace(t *testing.T) {
	setLifecycleDeadline(t, 25*time.Millisecond)
	dir := privateRuntime(t)
	requestPath := filepath.Join(dir, requestName)
	if err := writeLaunchRequest(requestPath, LaunchRequest{Binding: testBinding(), RuntimeDirectory: dir, HostConfigPath: "/private/config", SessionRecordName: "dev", Host: testHostExpectation(), CA: testCAExpectation()}); err != nil {
		t.Fatal(err)
	}
	if err := writeBoundGenerationLock(filepath.Join(dir, lockName), LaunchRequest{Binding: testBinding(), RuntimeDirectory: dir, HostConfigPath: "/private/config", SessionRecordName: "dev", Host: testHostExpectation(), CA: testCAExpectation()}); err != nil {
		t.Fatal(err)
	}
	identity := ProcessIdentity{PID: os.Getpid(), StartedAt: time.Unix(602, 0).UTC(), Unique: 62}
	owner := startResultOwner{testOwner: newTestOwner(identity), result: RuntimeStartResult{Owned: true}, err: errors.New("start retained runtime ownership")}
	done := make(chan error, 1)
	go func() { done <- Run(context.Background(), requestPath, owner, testInspector(identity)) }()
	select {
	case err := <-done:
		t.Fatalf("partial start returned before exact reaping: %v", err)
	case <-time.After(2 * lifecycleDeadline()):
	}
	if owner.closeCalls() != 0 {
		t.Fatal("partial start closed before its reaper completed")
	}
	owner.exit(nil)
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "start retained runtime ownership") {
			t.Fatalf("Run() error=%v, want partial start cause", err)
		}
	case <-time.After(time.Second):
		t.Fatal("partial start did not complete after reaping")
	}
	if owner.stopCalls() != 1 || owner.closeCalls() != 1 {
		t.Fatalf("partial start Stop=%d Close=%d, want one each", owner.stopCalls(), owner.closeCalls())
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
	if err := writeLaunchRequest(requestPath, LaunchRequest{Binding: testBinding(), RuntimeDirectory: dir, HostConfigPath: "/private/config", SessionRecordName: "dev", Host: testHostExpectation(), CA: testCAExpectation()}); err != nil {
		t.Fatal(err)
	}
	owner := newTestOwner(ProcessIdentity{PID: 1, StartedAt: time.Unix(1, 0), Unique: 1})
	runtimeIdentity, err := capturePrivateDirectory(dir)
	if err != nil {
		t.Fatal(err)
	}
	request, err := readLaunchRequest(requestPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := prepare(context.Background(), request, owner, unsupportedInspector{}, runtimeIdentity); err == nil {
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
	dir := privateLaunchRuntime(t)
	identity := ProcessIdentity{PID: os.Getpid(), StartedAt: time.Unix(300, 0), Unique: 30}
	var got LaunchCommand
	child := &testLaunchChild{}
	launcher := newDetachedLauncher(launcherDeps{executable: func() (string, error) { return "/private/boxwarden", nil }, inspector: testInspector(identity), start: func(_ context.Context, command LaunchCommand) (launchChild, error) {
		got = command
		if command.GenerationLock == nil {
			t.Fatal("detached launch did not receive the fixed inherited generation-lock capability")
		}
		contender, err := os.OpenFile(filepath.Join(dir, lockName), os.O_RDONLY, 0)
		if err != nil {
			t.Fatal(err)
		}
		defer contender.Close()
		if err := syscall.Flock(int(contender.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err == nil {
			t.Fatal("parent did not claim generation lock before fake child start")
		}
		return child, nil
	}, await: func(context.Context, *Client, Binding) error { return nil }})
	request := LaunchRequest{Binding: testBinding(), RuntimeDirectory: dir, HostConfigPath: "/private/config", SessionRecordName: "dev", Host: testHostExpectation(), CA: testCAExpectation()}
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

func TestDetachedLauncherReturnsOwnedGenerationWithoutSpawnOrCleanupOnLockContention(t *testing.T) {
	dir := privateRuntime(t)
	request := LaunchRequest{Binding: testBinding(), RuntimeDirectory: dir, HostConfigPath: "/private/config", SessionRecordName: "dev", Host: testHostExpectation(), CA: testCAExpectation()}
	if err := writeLaunchRequest(filepath.Join(dir, requestName), request); err != nil {
		t.Fatal(err)
	}
	if err := writeBoundGenerationLock(filepath.Join(dir, lockName), request); err != nil {
		t.Fatal(err)
	}
	held, err := os.OpenFile(filepath.Join(dir, lockName), os.O_RDONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	if err := syscall.Flock(int(held.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	called := false
	identity := ProcessIdentity{PID: os.Getpid(), StartedAt: time.Unix(301, 0), Unique: 31}
	launcher := newDetachedLauncher(launcherDeps{executable: func() (string, error) { return "/private/boxwarden", nil }, inspector: testInspector(identity), start: func(context.Context, LaunchCommand) (launchChild, error) {
		called = true
		return &testLaunchChild{}, nil
	}, await: func(context.Context, *Client, Binding) error { return nil }})
	err = launcher.Launch(context.Background(), request)
	if !errors.Is(err, errGenerationAlreadyOwned) {
		t.Fatalf("Launch() error=%v, want owned-generation sentinel", err)
	}
	if called {
		t.Fatal("contended generation spawned a child")
	}
	if _, err := os.Lstat(filepath.Join(dir, requestName)); err != nil {
		t.Fatalf("contended request was cleaned: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(dir, lockName)); err != nil {
		t.Fatalf("contended lock was cleaned: %v", err)
	}
}

type testLaunchChild struct {
	released, stopped bool
	releaseErr        error
	waitErr           error
	waitRelease       chan struct{}
	stopCalls         int
	waitCalls         int
}

func (c *testLaunchChild) release() error { c.released = true; return c.releaseErr }
func (c *testLaunchChild) stop() error {
	c.stopped = true
	c.stopCalls++
	return nil
}
func (c *testLaunchChild) wait() error {
	c.waitCalls++
	if c.waitRelease != nil {
		<-c.waitRelease
	}
	return c.waitErr
}

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

func TestControlStopLeavesBoundedResponseWindow(t *testing.T) {
	setLifecycleDeadline(t, 120*time.Millisecond)
	owner := newTestOwner(ProcessIdentity{PID: 1, StartedAt: time.Unix(1, 0), Unique: 1})
	key := make([]byte, 32)
	key[0] = 2
	manifest := Manifest{Binding: testBinding()}
	client, server := net.Pipe()
	defer client.Close()
	go handleControl(server, manifest, key, owner, func() error {
		time.Sleep(90 * time.Millisecond)
		return nil
	})
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
	time.Sleep(60 * time.Millisecond)
	if err := writeFrame(client, data); err != nil {
		t.Fatal(err)
	}
	_ = client.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
	responseData, err := readBounded(client)
	if err != nil {
		t.Fatalf("read stop response after bounded parsing and stop: %v", err)
	}
	var response controlResponse
	if err := decodeExact(responseData, &response); err != nil {
		t.Fatal(err)
	}
	want, err := responseMAC(key, response)
	if err != nil || response.MAC != want || response.Error != "" {
		t.Fatalf("stop response = %#v, MAC error = %v", response, err)
	}
}

func TestStopClientDeadlineCoversServerLifecycleWindow(t *testing.T) {
	setLifecycleDeadline(t, 3*time.Second)
	service, controller, cancel := runningService(t, false)
	defer cancel()
	go func() {
		time.Sleep(2200 * time.Millisecond)
		service.owner.exit(nil)
	}()
	ctx, cancelStop := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelStop()
	if err := controller.Stop(ctx, testBinding()); err != nil {
		t.Fatalf("Stop() expired before the server lifecycle window: %v", err)
	}
	select {
	case <-service.done:
	case <-time.After(time.Second):
		t.Fatal("supervisor did not finish after valid stop")
	}
}

func TestStopClientStartsActionWindowAfterDelayedDial(t *testing.T) {
	// This catches starting the complete stop window before Unix connect. A
	// 300 ms pre-accept delay plus a valid 100 ms stop exceeds the former
	// 360 ms single budget (120 ms lifecycle + two I/O intervals), while the
	// post-connect operation remains inside its authorized window.
	setLifecycleDeadline(t, 120*time.Millisecond)
	service, controller, cancel := runningService(t, false)
	defer cancel()
	previousDial := controlDialContext
	controlDialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		timer := time.NewTimer(300 * time.Millisecond)
		defer timer.Stop()
		select {
		case <-timer.C:
			return (&net.Dialer{}).DialContext(ctx, network, address)
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	t.Cleanup(func() { controlDialContext = previousDial })
	go func() {
		time.Sleep(400 * time.Millisecond)
		service.owner.exit(nil)
	}()
	if err := controller.Stop(context.Background(), testBinding()); err != nil {
		t.Fatalf("Stop() spent pre-accept time from its action window: %v", err)
	}
	select {
	case <-service.done:
	case <-time.After(time.Second):
		t.Fatal("supervisor did not finish after a valid delayed-dial stop")
	}
}

func TestOwnerReaperCompletionWinsReadyDeadline(t *testing.T) {
	terminal := errors.New("owner terminal wait result")
	owner := newTestOwner(ProcessIdentity{PID: 1, StartedAt: time.Unix(605, 0).UTC(), Unique: 65})
	reaper := &ownerReaper{owner: owner, done: make(chan struct{})}
	reaper.start()
	owner.exit(terminal)
	select {
	case <-reaper.done:
	case <-time.After(time.Second):
		t.Fatal("owner reaper did not publish its terminal result")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for range 200 {
		awaited := reaper.await(ctx)
		if !awaited.completed || !errors.Is(awaited.err, terminal) {
			t.Fatalf("ready owner completion lost to deadline: %#v", awaited)
		}
	}
	dir := privateRuntime(t)
	requestPath := filepath.Join(dir, requestName)
	if err := writePrivateFile(requestPath, []byte("request")); err != nil {
		t.Fatal(err)
	}
	request, err := capturePrivateRegular(requestPath)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := capturePrivateDirectory(dir)
	if err != nil {
		t.Fatal(err)
	}
	setLifecycleDeadline(t, 0)
	err = terminateAndCleanup(owner, reaper, true, lifecycleResources{request: request, runtime: runtime})
	if strings.Count(err.Error(), terminal.Error()) != 1 || strings.Contains(err.Error(), "did not reap before cleanup deadline") {
		t.Fatalf("owner cleanup error=%v, want one terminal result and no synthetic timeout", err)
	}
	if owner.stopCalls() != 1 || owner.waitCalls() != 1 || owner.closeCalls() != 1 {
		t.Fatalf("owner lifecycle Stop=%d Wait=%d Close=%d, want exactly one each", owner.stopCalls(), owner.waitCalls(), owner.closeCalls())
	}
	if _, err := os.Lstat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("owner cleanup retained namespace: %v", err)
	}
}

func TestDetachedChildReaperCompletionWinsReadyDeadline(t *testing.T) {
	terminal := errors.New("detached child terminal wait result")
	child := &testLaunchChild{waitErr: terminal}
	if err := child.stop(); err != nil {
		t.Fatal(err)
	}
	reaper := &launchChildReaper{child: child, done: make(chan struct{})}
	reaper.start()
	select {
	case <-reaper.done:
	case <-time.After(time.Second):
		t.Fatal("child reaper did not publish its terminal result")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for range 200 {
		awaited := reaper.await(ctx)
		if !awaited.completed || !errors.Is(awaited.err, terminal) {
			t.Fatalf("ready child completion lost to deadline: %#v", awaited)
		}
	}
	if child.stopCalls != 1 || child.waitCalls != 1 || child.released {
		t.Fatalf("child lifecycle Stop=%d Wait=%d released=%t, want one retained stop/wait", child.stopCalls, child.waitCalls, child.released)
	}
}

func TestCleanupPreservesSameModeManifestReplacement(t *testing.T) {
	service, _, cancel := runningService(t, false)
	manifestPath := filepath.Join(service.dir, manifestName)
	requestPath := filepath.Join(service.dir, requestName)
	original, err := capturePrivateRegular(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	requestOriginal, err := capturePrivateRegular(requestPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(manifestPath); err != nil {
		t.Fatal(err)
	}
	if err := writePrivateFile(manifestPath, []byte("replacement")); err != nil {
		t.Fatal(err)
	}
	replacement, err := capturePrivateRegular(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if original.matches(replacement) {
		t.Fatal("replacement reused the owned manifest inode")
	}
	if err := os.Remove(requestPath); err != nil {
		t.Fatal(err)
	}
	if err := writePrivateFile(requestPath, []byte("request replacement")); err != nil {
		t.Fatal(err)
	}
	requestReplacement, err := capturePrivateRegular(requestPath)
	if err != nil {
		t.Fatal(err)
	}
	if requestOriginal.matches(requestReplacement) {
		t.Fatal("replacement reused the owned request inode")
	}
	cancel()
	service.owner.exit(nil)
	select {
	case <-service.done:
	case <-time.After(time.Second):
		t.Fatal("supervisor did not finish cleanup")
	}
	if _, err := os.Lstat(manifestPath); err != nil {
		t.Fatalf("same-mode replacement was removed: %v", err)
	}
	if _, err := os.Lstat(requestPath); err != nil {
		t.Fatalf("same-mode request replacement was removed: %v", err)
	}
}

func TestAdmittedPrivateRegularRetainsOriginalInodeUntilClosed(t *testing.T) {
	dir := privateRuntime(t)
	path := filepath.Join(dir, requestName)
	if err := writePrivateFile(path, []byte("original")); err != nil {
		t.Fatal(err)
	}
	retained, err := admitPrivateRegular(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := writePrivateFile(path, []byte("replacement")); err != nil {
		t.Fatal(err)
	}
	replacement, err := capturePrivateRegular(path)
	if err != nil {
		t.Fatal(err)
	}
	if retained.identity.matches(replacement) {
		t.Fatal("replacement reused inode while original artifact remained retained")
	}
	if err := retained.close(); err != nil {
		t.Fatal(err)
	}
	if retained.file != nil {
		t.Fatal("retained original file descriptor remained open after close")
	}
}

func TestAdmitLaunchRequestRejectsHardLinkSymlinkSubstitution(t *testing.T) {
	dir := privateRuntime(t)
	path := filepath.Join(dir, requestName)
	if err := writeLaunchRequest(path, LaunchRequest{Binding: testBinding(), RuntimeDirectory: dir, HostConfigPath: "/private/config", SessionRecordName: "dev", Host: testHostExpectation(), CA: testCAExpectation()}); err != nil {
		t.Fatal(err)
	}
	privateRegularAdmissionHook = func() { replaceWithHardLinkedSymlink(t, path) }
	t.Cleanup(func() { privateRegularAdmissionHook = nil })
	if _, retained, err := admitLaunchRequest(path); err == nil {
		_ = retained.close()
		t.Fatal("hard-link-backed request symlink was admitted")
	} else if retained != nil {
		t.Fatal("rejected request retained a file descriptor")
	}
}

func TestAdmitManifestRejectsHardLinkSymlinkSubstitution(t *testing.T) {
	dir := privateRuntime(t)
	request := LaunchRequest{Binding: testBinding(), RuntimeDirectory: dir, HostConfigPath: "/private/config", SessionRecordName: "dev", Host: testHostExpectation(), CA: testCAExpectation()}
	if err := writeLaunchRequest(filepath.Join(dir, requestName), request); err != nil {
		t.Fatal(err)
	}
	runtime, err := capturePrivateDirectory(dir)
	if err != nil {
		t.Fatal(err)
	}
	identity := ProcessIdentity{PID: os.Getpid(), StartedAt: time.Unix(607, 0).UTC(), Unique: 67}
	if _, started, err := prepare(context.Background(), request, newTestOwner(identity), testInspector(identity), runtime); err != nil || !started {
		t.Fatalf("prepare manifest err=%v started=%t", err, started)
	}
	path := filepath.Join(dir, manifestName)
	privateRegularAdmissionHook = func() { replaceWithHardLinkedSymlink(t, path) }
	t.Cleanup(func() { privateRegularAdmissionHook = nil })
	if _, retained, err := admitManifest(path); err == nil {
		_ = retained.close()
		t.Fatal("hard-link-backed manifest symlink was admitted")
	} else if retained != nil {
		t.Fatal("rejected manifest retained a file descriptor")
	}
}

func replaceWithHardLinkedSymlink(t *testing.T, path string) {
	t.Helper()
	hardLink := path + ".hard-link"
	if err := os.Link(path, hardLink); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Base(hardLink), path); err != nil {
		t.Fatal(err)
	}
}

func TestDetachedLauncherRetainsRequestInodeThroughFailedLaunchCleanup(t *testing.T) {
	dir := privateLaunchRuntime(t)
	identity := ProcessIdentity{PID: os.Getpid(), StartedAt: time.Unix(606, 0).UTC(), Unique: 66}
	var original FileIdentity
	launcher := newDetachedLauncher(launcherDeps{
		executable: func() (string, error) { return "/private/boxwarden", nil },
		inspector:  testInspector(identity),
		start: func(context.Context, LaunchCommand) (launchChild, error) {
			requestPath := filepath.Join(dir, requestName)
			var err error
			original, err = capturePrivateRegular(requestPath)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(requestPath); err != nil {
				t.Fatal(err)
			}
			if err := writePrivateFile(requestPath, []byte("replacement")); err != nil {
				t.Fatal(err)
			}
			return nil, errors.New("start failed after request replacement")
		},
		await: func(context.Context, *Client, Binding) error { return nil },
	})
	err := launcher.Launch(context.Background(), LaunchRequest{Binding: testBinding(), RuntimeDirectory: dir, HostConfigPath: "/private/config", SessionRecordName: "dev", Host: testHostExpectation(), CA: testCAExpectation()})
	if err == nil {
		t.Fatal("replacement launch failure was accepted")
	}
	replacement, captureErr := capturePrivateRegular(filepath.Join(dir, requestName))
	if captureErr != nil {
		t.Fatalf("parent cleanup removed request replacement: %v", captureErr)
	}
	if original.matches(replacement) {
		t.Fatal("parent request replacement reused the retained inode")
	}
}

func TestSuccessfulStopWaitsForHeldOwnerBeforeClosingNamespace(t *testing.T) {
	service, controller, cancel := runningService(t, false)
	defer cancel()
	stopped := make(chan error, 1)
	go func() { stopped <- controller.Stop(context.Background(), testBinding()) }()
	deadline := time.Now().Add(100 * time.Millisecond)
	for time.Now().Before(deadline) {
		if service.owner.closeCalls() != 0 {
			t.Fatal("supervisor closed runtime before held owner was reaped")
		}
		time.Sleep(time.Millisecond)
	}
	service.owner.exit(nil)
	if err := <-stopped; err != nil {
		t.Fatal(err)
	}
	select {
	case <-service.done:
	case <-time.After(time.Second):
		t.Fatal("supervisor did not close after owner exit")
	}
}

func TestFinalCleanupContinuesAfterStopFailureOnceReaped(t *testing.T) {
	setLifecycleDeadline(t, 25*time.Millisecond)
	service, _, cancel := runningService(t, false)
	service.owner.stopFailure = errors.New("stop failed")
	cancel()
	select {
	case <-service.done:
		t.Fatal("stop failure ended ownership before reaping")
	case <-time.After(2 * lifecycleDeadline()):
	}
	service.owner.exit(nil)
	select {
	case <-service.done:
	case <-time.After(time.Second):
		t.Fatal("final cleanup did not continue after reaping")
	}
	if service.owner.stopCalls() != 1 || service.owner.closeCalls() != 1 {
		t.Fatalf("Stop=%d Close=%d, want one exact final cleanup sequence", service.owner.stopCalls(), service.owner.closeCalls())
	}
}

func TestFinishStartedClosesExactlyOnceAfterStopFailureAndReap(t *testing.T) {
	owner := newTestOwner(ProcessIdentity{PID: 1, StartedAt: time.Unix(603, 0).UTC(), Unique: 63})
	owner.stopFailure = errors.New("stop failed")
	reaper := &ownerReaper{owner: &onceOwner{owner: owner}, done: make(chan struct{})}
	reaper.start()
	owner.exit(nil)
	err := finishStarted(errors.New("initial failure"), reaper.owner, reaper, true, lifecycleResources{})
	if err == nil || !strings.Contains(err.Error(), "stop failed") {
		t.Fatalf("finishStarted error=%v, want retained Stop failure", err)
	}
	if owner.stopCalls() != 1 || owner.closeCalls() != 1 {
		t.Fatalf("Stop=%d Close=%d, want one after proof of reap", owner.stopCalls(), owner.closeCalls())
	}
}

func TestDetachedLauncherReapsOnlyUnauthenticatedChildAndOwnRequest(t *testing.T) {
	dir := privateLaunchRuntime(t)
	identity := ProcessIdentity{PID: os.Getpid(), StartedAt: time.Unix(400, 0).UTC(), Unique: 40}
	child := &testLaunchChild{}
	launcher := newDetachedLauncher(launcherDeps{
		executable: func() (string, error) { return "/private/boxwarden", nil },
		inspector:  testInspector(identity),
		start:      func(context.Context, LaunchCommand) (launchChild, error) { return child, nil },
		await:      func(context.Context, *Client, Binding) error { return errors.New("no authenticated evidence") },
	})
	err := launcher.Launch(context.Background(), LaunchRequest{Binding: testBinding(), RuntimeDirectory: dir, HostConfigPath: "/private/config", SessionRecordName: "dev", Host: testHostExpectation(), CA: testCAExpectation()})
	if err == nil || !child.stopped || child.released {
		t.Fatalf("failed launch = %v, stopped=%t released=%t; want reaped but not released", err, child.stopped, child.released)
	}
	if _, err := os.Lstat(filepath.Join(dir, requestName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed launch request remains: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(dir, lockName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed launch bound lock remains: %v", err)
	}
}

// Production break: completing an exact request-only recovery turns the
// namespace into this launch attempt's proven supervisor state. A pre-detach
// failure must remove the retained request and lock and then the proven empty
// directory, not strand an empty generation for a later ambiguous retry.
func TestDetachedLauncherCleansCompletedRequestOnlyRecoveryNamespace(t *testing.T) {
	dir := privateRuntime(t)
	identity := ProcessIdentity{PID: os.Getpid(), StartedAt: time.Unix(405, 0).UTC(), Unique: 45}
	request := LaunchRequest{Binding: testBinding(), RuntimeDirectory: dir, HostConfigPath: "/private/config", SessionRecordName: "dev", Host: testHostExpectation(), CA: testCAExpectation()}
	if err := writeLaunchRequest(filepath.Join(dir, requestName), request); err != nil {
		t.Fatal(err)
	}
	launcher := newDetachedLauncher(launcherDeps{
		executable: func() (string, error) { return "/private/boxwarden", nil },
		inspector:  testInspector(identity),
		start: func(context.Context, LaunchCommand) (launchChild, error) {
			return nil, errors.New("recovered child did not start")
		},
		await: func(context.Context, *Client, Binding) error { return nil },
	})
	if err := launcher.Launch(context.Background(), request); err == nil {
		t.Fatal("request-only recovery launch failure was accepted")
	}
	if _, err := os.Lstat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed request-only recovery left generation namespace: %v", err)
	}
}

func TestDetachedLauncherRefusesCancelledContextBeforeRequestOrSpawn(t *testing.T) {
	dir := privateRuntime(t)
	identity := ProcessIdentity{PID: os.Getpid(), StartedAt: time.Unix(401, 0).UTC(), Unique: 41}
	called := false
	launcher := newDetachedLauncher(launcherDeps{
		executable: func() (string, error) { return "/private/boxwarden", nil },
		inspector:  testInspector(identity),
		start: func(context.Context, LaunchCommand) (launchChild, error) {
			called = true
			return &testLaunchChild{}, nil
		},
		await: func(context.Context, *Client, Binding) error { return nil },
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := launcher.Launch(ctx, LaunchRequest{Binding: testBinding(), RuntimeDirectory: dir, HostConfigPath: "/private/config", SessionRecordName: "dev", Host: testHostExpectation(), CA: testCAExpectation()}); !errors.Is(err, context.Canceled) {
		t.Fatalf("Launch() error = %v, want context cancellation", err)
	}
	if called {
		t.Fatal("cancelled launch reached child starter")
	}
	if _, err := os.Lstat(filepath.Join(dir, requestName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("cancelled launch wrote request: %v", err)
	}
}

func TestDetachedLauncherJoinsReleaseAndCleanupFailures(t *testing.T) {
	dir := privateLaunchRuntime(t)
	identity := ProcessIdentity{PID: os.Getpid(), StartedAt: time.Unix(402, 0).UTC(), Unique: 42}
	child := &testLaunchChild{releaseErr: errors.New("release failed"), waitErr: errors.New("reap failed")}
	launcher := newDetachedLauncher(launcherDeps{
		executable: func() (string, error) { return "/private/boxwarden", nil },
		inspector:  testInspector(identity),
		start:      func(context.Context, LaunchCommand) (launchChild, error) { return child, nil },
		await:      func(context.Context, *Client, Binding) error { return nil },
	})
	err := launcher.Launch(context.Background(), LaunchRequest{Binding: testBinding(), RuntimeDirectory: dir, HostConfigPath: "/private/config", SessionRecordName: "dev", Host: testHostExpectation(), CA: testCAExpectation()})
	if err == nil || !strings.Contains(err.Error(), "release failed") || !strings.Contains(err.Error(), "reap failed") || !child.stopped {
		t.Fatalf("Launch() error = %v, child=%#v; want joined release/reap failure", err, child)
	}
	if _, statErr := os.Lstat(filepath.Join(dir, requestName)); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("failed release request remains: %v", statErr)
	}
}

func TestDetachedLauncherReturnsTerminalWaitErrorOnce(t *testing.T) {
	dir := privateLaunchRuntime(t)
	identity := ProcessIdentity{PID: os.Getpid(), StartedAt: time.Unix(404, 0).UTC(), Unique: 44}
	waitErr := errors.New("detached child terminal wait failure")
	child := &testLaunchChild{waitErr: waitErr}
	launcher := newDetachedLauncher(launcherDeps{
		executable: func() (string, error) { return "/private/boxwarden", nil },
		inspector:  testInspector(identity),
		start:      func(context.Context, LaunchCommand) (launchChild, error) { return child, nil },
		await:      func(context.Context, *Client, Binding) error { return errors.New("authentication failed") },
	})
	err := launcher.Launch(context.Background(), LaunchRequest{Binding: testBinding(), RuntimeDirectory: dir, HostConfigPath: "/private/config", SessionRecordName: "dev", Host: testHostExpectation(), CA: testCAExpectation()})
	if err == nil || strings.Count(err.Error(), waitErr.Error()) != 1 {
		t.Fatalf("Launch() error = %v, want one terminal Wait result", err)
	}
	if child.stopCalls != 1 || child.waitCalls != 1 || child.released {
		t.Fatalf("child lifecycle = %#v, want one Stop, one Wait, and no release", child)
	}
	if _, statErr := os.Lstat(filepath.Join(dir, requestName)); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("failed launch request remains: %v", statErr)
	}
}

func TestDetachedLauncherRetainsRequestUntilBlockedReaperCompletes(t *testing.T) {
	setLifecycleDeadline(t, 25*time.Millisecond)
	dir := privateLaunchRuntime(t)
	identity := ProcessIdentity{PID: os.Getpid(), StartedAt: time.Unix(403, 0).UTC(), Unique: 43}
	child := &testLaunchChild{waitRelease: make(chan struct{})}
	launcher := newDetachedLauncher(launcherDeps{
		executable: func() (string, error) { return "/private/boxwarden", nil },
		inspector:  testInspector(identity),
		start:      func(context.Context, LaunchCommand) (launchChild, error) { return child, nil },
		await:      func(context.Context, *Client, Binding) error { return errors.New("no authenticated evidence") },
	})
	done := make(chan error, 1)
	go func() {
		done <- launcher.Launch(context.Background(), LaunchRequest{Binding: testBinding(), RuntimeDirectory: dir, HostConfigPath: "/private/config", SessionRecordName: "dev", Host: testHostExpectation(), CA: testCAExpectation()})
	}()
	select {
	case err := <-done:
		t.Fatalf("Launch returned before the retained reaper completed: %v", err)
	case <-time.After(2 * lifecycleDeadline()):
	}
	if _, err := os.Lstat(filepath.Join(dir, requestName)); err != nil {
		t.Fatalf("blocked reaper lost exact request ownership: %v", err)
	}
	close(child.waitRelease)
	select {
	case err := <-done:
		if err == nil || !child.stopped || child.stopCalls != 1 || child.waitCalls != 1 {
			t.Fatalf("Launch result=%v child=%#v, want causal error after exact reap", err, child)
		}
	case <-time.After(time.Second):
		t.Fatal("Launch did not complete after its one reaper completed")
	}
	if _, err := os.Lstat(filepath.Join(dir, requestName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("request remains after reaper completion: %v", err)
	}
}

func TestDetachedLauncherRefusesUnsupportedPlatformBeforeRequestMutation(t *testing.T) {
	dir := privateRuntime(t)
	launcher := newDetachedLauncher(launcherDeps{inspector: unsupportedInspector{}})
	err := launcher.Launch(context.Background(), LaunchRequest{Binding: testBinding(), RuntimeDirectory: dir, HostConfigPath: "/private/config", SessionRecordName: "dev", Host: testHostExpectation(), CA: testCAExpectation()})
	if err == nil {
		t.Fatal("unsupported platform launched")
	}
	if _, err := os.Lstat(filepath.Join(dir, requestName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unsupported platform wrote request: %v", err)
	}
}

func TestGenerationLockRejectsSameUIDSymlinkAndContention(t *testing.T) {
	dir := privateRuntime(t)
	request := LaunchRequest{Binding: testBinding(), RuntimeDirectory: dir, HostConfigPath: "/private/config", SessionRecordName: "dev", Host: testHostExpectation(), CA: testCAExpectation()}
	target := filepath.Join(dir, "same-uid-target")
	if err := writePrivateFile(target, []byte("target")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(dir, lockName)); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if _, err := acquirePublishedGenerationLockInRoot(root, dir, request); err == nil {
		t.Fatal("same-UID 0600 lock symlink was admitted")
	}
	if err := os.Remove(filepath.Join(dir, lockName)); err != nil {
		t.Fatal(err)
	}
	if err := writeBoundGenerationLock(filepath.Join(dir, lockName), request); err != nil {
		t.Fatal(err)
	}
	first, err := acquirePublishedGenerationLockInRoot(root, dir, request)
	if err != nil {
		t.Fatal(err)
	}
	defer first.close()
	if _, err := acquirePublishedGenerationLockInRoot(root, dir, request); err == nil {
		t.Fatal("second lock owner was admitted")
	}
}

func TestRootedLockAdmissionPreservesReplacement(t *testing.T) {
	dir := privateRuntime(t)
	request := LaunchRequest{Binding: testBinding(), RuntimeDirectory: dir, HostConfigPath: "/private/config", SessionRecordName: "dev", Host: testHostExpectation(), CA: testCAExpectation()}
	if err := writeBoundGenerationLock(filepath.Join(dir, lockName), request); err != nil {
		t.Fatal(err)
	}
	lockAdmissionHook = func() {
		if err := os.Remove(filepath.Join(dir, lockName)); err != nil {
			t.Fatalf("replace admitted lock: %v", err)
		}
		if err := writePrivateFile(filepath.Join(dir, lockName), []byte("replacement")); err != nil {
			t.Fatalf("write lock replacement: %v", err)
		}
	}
	t.Cleanup(func() { lockAdmissionHook = nil })
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if _, err := acquirePublishedGenerationLockInRoot(root, dir, request); err == nil {
		t.Fatal("rooted admission accepted a replaced lock")
	}
	if _, err := os.Lstat(filepath.Join(dir, lockName)); err != nil {
		t.Fatalf("rooted admission removed replacement: %v", err)
	}
}

// Production break: the detached child must claim only the already-published
// request-bound lock. It may not turn a missing lock into an unbound file, and
// no runtime owner may start before that admission succeeds.
func TestRunRequiresExactPublishedBoundLockBeforeRuntimeStart(t *testing.T) {
	for _, scenario := range []struct {
		name string
		lock func(t *testing.T, request LaunchRequest)
	}{
		{name: "missing"},
		{
			name: "foreign",
			lock: func(t *testing.T, request LaunchRequest) {
				t.Helper()
				foreign := request
				foreign.Binding.BackendObject = "object-other"
				if err := writeBoundGenerationLock(filepath.Join(request.RuntimeDirectory, lockName), foreign); err != nil {
					t.Fatal(err)
				}
			},
		},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			dir := privateRuntime(t)
			request := LaunchRequest{Binding: testBinding(), RuntimeDirectory: dir, HostConfigPath: "/private/config", SessionRecordName: "dev", Host: testHostExpectation(), CA: testCAExpectation()}
			if err := writeLaunchRequest(filepath.Join(dir, requestName), request); err != nil {
				t.Fatal(err)
			}
			if scenario.lock != nil {
				scenario.lock(t, request)
			}
			identity := ProcessIdentity{PID: 1, StartedAt: time.Unix(1, 0).UTC(), Unique: 1}
			owner := startResultOwner{testOwner: newTestOwner(identity), err: errors.New("runtime owner must not start")}
			if err := Run(context.Background(), filepath.Join(dir, requestName), owner, testInspector(identity)); err == nil {
				t.Fatal("Run() accepted missing or foreign generation lock")
			}
			if owner.started() {
				t.Fatal("Run() reached runtime start before exact bound lock admission")
			}
		})
	}
}

// Production break: the child admission helper must have no create surface.
// A missing generation.lock is drift, and must remain absent after refusal.
func TestAcquirePublishedGenerationLockRefusesMissingLockWithoutCreation(t *testing.T) {
	dir := privateRuntime(t)
	request := LaunchRequest{Binding: testBinding(), RuntimeDirectory: dir, HostConfigPath: "/private/config", SessionRecordName: "dev", Host: testHostExpectation(), CA: testCAExpectation()}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if _, err := acquirePublishedGenerationLockInRoot(root, dir, request); err == nil {
		t.Fatal("missing generation.lock was acquired")
	}
	if _, err := os.Lstat(filepath.Join(dir, lockName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing generation lock was created during child admission: %v", err)
	}
}

func TestRootedRuntimeAdmissionRejectsDirectoryReplacement(t *testing.T) {
	dir := privateRuntime(t)
	requestPath := filepath.Join(dir, requestName)
	if err := writeLaunchRequest(requestPath, LaunchRequest{Binding: testBinding(), RuntimeDirectory: dir, HostConfigPath: "/private/config", SessionRecordName: "dev", Host: testHostExpectation(), CA: testCAExpectation()}); err != nil {
		t.Fatal(err)
	}
	if err := writeBoundGenerationLock(filepath.Join(dir, lockName), LaunchRequest{Binding: testBinding(), RuntimeDirectory: dir, HostConfigPath: "/private/config", SessionRecordName: "dev", Host: testHostExpectation(), CA: testCAExpectation()}); err != nil {
		t.Fatal(err)
	}
	backup := dir + "-original"
	runtimeAdmissionHook = func() {
		if err := os.Rename(dir, backup); err != nil {
			t.Fatalf("replace runtime directory: %v", err)
		}
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatalf("create runtime replacement: %v", err)
		}
	}
	t.Cleanup(func() { runtimeAdmissionHook = nil; _ = os.RemoveAll(dir); _ = os.Rename(backup, dir) })
	if err := Run(context.Background(), requestPath, newTestOwner(ProcessIdentity{PID: 1, StartedAt: time.Unix(1, 0), Unique: 1}), testInspector(ProcessIdentity{PID: os.Getpid(), StartedAt: time.Unix(1, 0), Unique: 1})); err == nil {
		t.Fatal("rooted runtime admission accepted a path replacement")
	}
}

func TestRuntimeEvidenceRequiresEachNamedRoleAndOneBrokerState(t *testing.T) {
	identity := ProcessIdentity{PID: 1, StartedAt: time.Unix(1, 0).UTC(), Unique: 1}
	dir := privateRuntime(t)
	if err := validStartEvidence(RuntimeStartEvidence{Children: []NamedProcessEvidence{{Role: "backend", Identity: identity}, {Role: "backend", Identity: identity}}, Endpoints: endpointEvidence(t, dir), Broker: BrokerEvidence{Healthy: true}}, dir); err == nil {
		t.Fatal("duplicate direct-child role was admitted")
	}
	if err := validStartEvidence(RuntimeStartEvidence{Children: []NamedProcessEvidence{{Role: "backend", Identity: identity}, {Role: "screen", Identity: identity}}, Endpoints: endpointEvidence(t, dir), Broker: BrokerEvidence{}}, dir); err == nil {
		t.Fatal("unknown initial broker state was admitted")
	}
}

func TestRuntimeEvidenceRejectsUnassociatedOrNonEndpointForms(t *testing.T) {
	dir := privateRuntime(t)
	identity := ProcessIdentity{PID: 1, StartedAt: time.Unix(1, 0).UTC(), Unique: 1}
	evidence := RuntimeStartEvidence{Children: []NamedProcessEvidence{{Role: "backend", Identity: identity}, {Role: "screen", Identity: identity}}, Endpoints: endpointEvidence(t, dir), Broker: BrokerEvidence{Healthy: true}}
	if err := validStartEvidence(evidence, dir); err != nil {
		t.Fatalf("valid endpoint evidence rejected: %v", err)
	}
	evidence.Endpoints[0].Identity.Path = "/private/unrelated/tart-serial"
	if err := validStartEvidence(evidence, dir); err == nil {
		t.Fatal("endpoint outside runtime was admitted")
	}
	evidence = RuntimeStartEvidence{Children: []NamedProcessEvidence{{Role: "backend", Identity: identity}, {Role: "screen", Identity: identity}}, Endpoints: endpointEvidence(t, dir), Broker: BrokerEvidence{Healthy: true}}
	if err := os.Remove(filepath.Join(dir, "serial", "operator-console")); err != nil {
		t.Fatal(err)
	}
	if err := writePrivateFile(filepath.Join(dir, "serial", "operator-console"), []byte("not a pty endpoint")); err != nil {
		t.Fatal(err)
	}
	operator, _, err := captureIdentity(filepath.Join(dir, "serial", "operator-console"))
	if err != nil {
		t.Fatal(err)
	}
	evidence.Endpoints[1].Identity = operator
	if err := validStartEvidence(evidence, dir); err == nil {
		t.Fatal("regular file endpoint was admitted")
	}
}

func TestListenerCloseAndIdentityCleanupPreserveSocketReplacement(t *testing.T) {
	dir := privateRuntime(t)
	path := filepath.Join(dir, socketName)
	listener, _, err := listenSocket(path)
	if err != nil {
		if strings.Contains(err.Error(), "operation not permitted") {
			t.Skipf("sandbox denies Unix socket: %v", err)
		}
		t.Fatal(err)
	}
	original, err := captureSocket(path)
	if err != nil {
		_ = listener.Close()
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		_ = listener.Close()
		t.Fatal(err)
	}
	replacement, _, err := listenSocket(path)
	if err != nil {
		_ = listener.Close()
		t.Fatal(err)
	}
	defer replacement.Close()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	if err := cleanupNamespace(lifecycleResources{socket: original}); err == nil {
		t.Fatal("socket replacement cleanup succeeded")
	}
	if _, err := os.Lstat(path); err != nil {
		t.Fatalf("listener close or cleanup removed socket replacement: %v", err)
	}
}

func TestListenSocketPostBindFailurePreservesReplacement(t *testing.T) {
	dir := privateRuntime(t)
	path := filepath.Join(dir, socketName)
	socketAdmissionHook = func() {
		if err := os.Remove(path); err != nil {
			t.Fatalf("remove just-bound socket: %v", err)
		}
		if err := writePrivateFile(path, []byte("replacement")); err != nil {
			t.Fatalf("install socket replacement: %v", err)
		}
	}
	t.Cleanup(func() { socketAdmissionHook = nil })
	listener, _, err := listenSocket(path)
	if err != nil {
		if strings.Contains(err.Error(), "operation not permitted") {
			t.Skipf("sandbox denies Unix socket: %v", err)
		}
		if !strings.Contains(err.Error(), "preserve replacement") {
			t.Fatalf("post-bind replacement error=%v, want visible preservation", err)
		}
	} else {
		_ = listener.Close()
		t.Fatal("post-bind replacement was admitted")
	}
	if _, err := os.Lstat(path); err != nil {
		t.Fatalf("post-bind cleanup removed replacement: %v", err)
	}
}

func TestListenSocketChmodFailureRemovesOnlyCapturedSocket(t *testing.T) {
	dir := privateRuntime(t)
	path := filepath.Join(dir, socketName)
	socketChmod = func(string, os.FileMode) error { return errors.New("chmod denied") }
	t.Cleanup(func() { socketChmod = os.Chmod })
	listener, _, err := listenSocket(path)
	if err != nil {
		if strings.Contains(err.Error(), "operation not permitted") {
			t.Skipf("sandbox denies Unix socket: %v", err)
		}
		if !strings.Contains(err.Error(), "chmod denied") {
			t.Fatalf("chmod failure error=%v, want original cause", err)
		}
	} else {
		_ = listener.Close()
		t.Fatal("chmod failure admitted a listener")
	}
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("chmod rollback socket=%v, want exact original absent", err)
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
	result    <-chan error
}

// Production break: a future authenticated observation cannot be fresh
// evidence because an untrusted child clock could otherwise extend its life.
func TestClientRejectsFutureAuthenticatedSnapshot(t *testing.T) {
	now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	if err := validateSnapshotFreshness(Snapshot{ObservedAt: now.Add(time.Second)}, now, time.Minute); err == nil {
		t.Fatal("validateSnapshotFreshness() accepted a future observation")
	}
	if err := validateSnapshotFreshness(Snapshot{ObservedAt: now}, now, time.Minute); err != nil {
		t.Fatalf("validateSnapshotFreshness() exact trusted-time boundary error = %v", err)
	}
}

// Production break: accepting duplicate nested JSON lets an attacker smuggle
// a second binding or CA field past the immutable request contract.
func TestDecodeExactRejectsDuplicateNestedFields(t *testing.T) {
	for name, data := range map[string][]byte{
		"request binding":       []byte(`{"binding":{"domain":"w","domain":"other"}}`),
		"request host manifest": []byte(`{"host":{"manifest":{"version":2,"version":3}}}`),
		"request CA":            []byte(`{"ca":{"domain":"w","domain":"other"}}`),
		"manifest evidence":     []byte(`{"evidence":{"broker":{"healthy":true,"healthy":false}}}`),
	} {
		t.Run(name, func(t *testing.T) {
			var value any
			if err := decodeExact(data, &value); err == nil {
				t.Fatal("decodeExact() accepted duplicate nested field")
			}
		})
	}
}

func runningService(t *testing.T, poisoned bool) (testService, *Client, context.CancelFunc) {
	t.Helper()
	dir := privateRuntime(t)
	identity := ProcessIdentity{PID: os.Getpid(), StartedAt: time.Unix(200, 0).UTC(), Unique: 9}
	owner := newTestOwner(identity)
	owner.poisoned = poisoned
	requestPath := filepath.Join(dir, requestName)
	if err := writeLaunchRequest(requestPath, LaunchRequest{Binding: testBinding(), RuntimeDirectory: dir, HostConfigPath: "/private/config", SessionRecordName: "dev", Host: testHostExpectation(), CA: testCAExpectation()}); err != nil {
		t.Fatal(err)
	}
	if err := writeBoundGenerationLock(filepath.Join(dir, lockName), LaunchRequest{Binding: testBinding(), RuntimeDirectory: dir, HostConfigPath: "/private/config", SessionRecordName: "dev", Host: testHostExpectation(), CA: testCAExpectation()}); err != nil {
		t.Fatal(err)
	}
	inspector := testInspector(identity)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	runErr := make(chan error, 1)
	go func() { defer close(done); runErr <- Run(ctx, requestPath, owner, inspector) }()
	t.Cleanup(func() {
		cancel()
		owner.exit(nil)
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Errorf("supervisor test owner did not finish during cleanup")
		}
	})
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
		owner.exit(nil)
		select {
		case err := <-runErr:
			if err != nil && strings.Contains(err.Error(), "operation not permitted") {
				t.Skipf("sandbox denies owner-private Unix socket: %v", err)
			}
			t.Fatalf("supervisor did not publish manifest: %v (socket=%#v stat=%v)", err, info, statErr)
		case <-time.After(time.Second):
			t.Fatalf("supervisor did not finish failed publication cleanup (socket=%#v stat=%v)", info, statErr)
		}
	}
	key, err := decodeKey(manifest.ControlKey)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	return testService{dir: dir, key: key, identity: identity, owner: owner, inspector: inspector, done: done, result: runErr}, client, cancel
}
func socketIsPrivate(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.Mode()&os.ModeSocket != 0 && info.Mode().Perm() == 0o600
}
func testBinding() Binding {
	return Binding{Domain: "w", SessionID: "s", BackendKind: "fake", BackendObject: "object-1", Generation: "g"}
}

func testHostExpectation() HostExpectation {
	return HostExpectation{Manifest: hostx.Manifest{
		Version: hostx.ManifestVersion, Platform: hostx.QualifiedPlatform, MacOS: hostx.QualifiedMacOS, MacOSBuild: hostx.QualifiedMacOSBuild,
		Tart:    hostx.ToolIdentity{Path: "/opt/qualified/tart", Version: hostx.TartVersion, ExecutableSHA256: hostx.TartExecutableSHA256, ArchiveSHA256: hostx.TartArchiveSHA256},
		Softnet: hostx.ToolIdentity{Path: hostx.QualifiedSoftnetPath, Version: hostx.SoftnetVersion, ExecutableSHA256: hostx.SoftnetExecutableSHA256, ArchiveSHA256: hostx.SoftnetArchiveSHA256},
		RootUID: 0, Group: hostx.Group{ID: 20, Name: hostx.OperatorGroupName, Members: []int{501}}, Operator: hostx.Operator{UID: 501, Name: "operator", Home: "/Users/operator"}, TartHome: "/Users/operator/tart", SoftnetMode: hostx.SoftnetMode, InstalledAt: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
	}, ScreenPath: hostx.ScreenPath, ScreenSHA256: hostx.ScreenExecutableSHA256, ScreenVersion: hostx.ScreenVersionOutput, SoftnetBinDir: filepath.Dir(hostx.QualifiedSoftnetPath)}
}

func testCAExpectation() CAExpectation {
	return CAExpectation{Version: 1, Domain: "w", Algorithm: "ssh-ed25519", PublicKey: "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGxvY2FsLWNhLXRlc3Q= boxwarden", PublicDigest: "digest", Fingerprint: "SHA256:fingerprint", CreationUUID: "11111111-2222-4333-8444-555555555555", CreatorUID: 501, CreatorName: "operator"}
}

func TestPrivateRuntimeUsesGoTemporaryDirectory(t *testing.T) {
	dir := privateRuntime(t)
	relative, err := filepath.Rel(os.TempDir(), dir)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
		t.Fatalf("private runtime %q is not under Go temporary directory %q: relative=%q err=%v", dir, os.TempDir(), relative, err)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !info.IsDir() || info.Mode().Perm() != 0o700 {
		t.Fatalf("private runtime mode=%#o directory=%t, want owner-private directory", info.Mode(), info.IsDir())
	}
}

func privateRuntime(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "bw-sup-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "w", "s", "g")
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func privateLaunchRuntime(t *testing.T) string {
	t.Helper()
	path := privateRuntime(t)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	return path
}

func setLifecycleDeadline(t *testing.T, value time.Duration) {
	t.Helper()
	previous := lifecycleDeadlineNanos.Swap(int64(value))
	t.Cleanup(func() { lifecycleDeadlineNanos.Store(previous) })
}

func endpointEvidence(t *testing.T, dir string) []NamedFileEvidence {
	t.Helper()
	serial := filepath.Join(dir, "serial")
	if err := os.MkdirAll(serial, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"tart-serial", "operator-console"} {
		target := filepath.Join(serial, name+"-target")
		endpoint := filepath.Join(serial, name)
		if _, err := os.Lstat(endpoint); errors.Is(err, os.ErrNotExist) {
			if err := writePrivateFile(target, []byte(name)); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(filepath.Base(target), endpoint); err != nil {
				t.Fatal(err)
			}
		}
	}
	tart, _, err := captureIdentity(filepath.Join(serial, "tart-serial"))
	if err != nil {
		t.Fatal(err)
	}
	operator, _, err := captureIdentity(filepath.Join(serial, "operator-console"))
	if err != nil {
		t.Fatal(err)
	}
	return []NamedFileEvidence{{Role: "tart-serial", Identity: tart}, {Role: "operator-console", Identity: operator}}
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
	stopped, waited, closed     int
	stopFailure                 error
	endpoints                   []string
	exitOnce                    sync.Once
}

type invalidEvidenceOwner struct{ *testOwner }

func (invalidEvidenceOwner) Start(context.Context, LaunchRequest) (RuntimeStartResult, error) {
	return RuntimeStartResult{Owned: true}, nil
}

type startResultOwner struct {
	*testOwner
	result RuntimeStartResult
	err    error
}

func (o startResultOwner) Start(context.Context, LaunchRequest) (RuntimeStartResult, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.didStart = true
	return o.result, o.err
}

func newTestOwner(identity ProcessIdentity) *testOwner {
	return &testOwner{identity: identity, healthy: true, exitCh: make(chan error, 1)}
}
func (o *testOwner) Start(_ context.Context, request LaunchRequest) (RuntimeStartResult, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.didStart = true
	endpoints := make([]NamedFileEvidence, 0, 2)
	serial := filepath.Join(request.RuntimeDirectory, "serial")
	if err := os.Mkdir(serial, 0o700); err != nil {
		return RuntimeStartResult{}, err
	}
	for _, name := range []string{"tart-serial", "operator-console"} {
		target := filepath.Join(serial, name+"-target")
		endpoint := filepath.Join(serial, name)
		if err := writePrivateFile(target, []byte(name)); err != nil {
			return RuntimeStartResult{}, err
		}
		if err := os.Symlink(filepath.Base(target), endpoint); err != nil {
			return RuntimeStartResult{}, err
		}
		identity, _, err := captureIdentity(endpoint)
		if err != nil {
			return RuntimeStartResult{}, err
		}
		endpoints = append(endpoints, NamedFileEvidence{Role: name, Identity: identity})
		o.endpoints = append(o.endpoints, endpoint, target)
	}
	return RuntimeStartResult{Owned: true, Evidence: RuntimeStartEvidence{Children: []NamedProcessEvidence{{Role: "backend", Identity: ProcessIdentity{PID: 72, StartedAt: time.Unix(201, 0).UTC(), Unique: 10}}, {Role: "screen", Identity: ProcessIdentity{PID: 73, StartedAt: time.Unix(202, 0).UTC(), Unique: 11}}}, Endpoints: endpoints, Broker: BrokerEvidence{Healthy: true}}}, nil
}
func (o *testOwner) Snapshot() Snapshot {
	o.mu.Lock()
	defer o.mu.Unlock()
	return Snapshot{BackendRunning: o.healthy, BrokerHealthy: o.healthy && !o.poisoned, ScreenHealthy: o.healthy, PinPresent: true, CertificateCurrent: true, ProbeOK: true, ZoneMatches: true, Diagnostic: string(make([]byte, maxDiagnosticBytes+10))}
}
func (o *testOwner) Wait(context.Context) error {
	o.mu.Lock()
	o.waited++
	o.mu.Unlock()
	return <-o.exitCh
}
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
func (o *testOwner) exit(err error)    { o.exitOnce.Do(func() { o.exitCh <- err }) }
func (o *testOwner) stopCalls() int    { o.mu.Lock(); defer o.mu.Unlock(); return o.stopped }
func (o *testOwner) waitCalls() int    { o.mu.Lock(); defer o.mu.Unlock(); return o.waited }
func (o *testOwner) closeCalls() int   { o.mu.Lock(); defer o.mu.Unlock(); return o.closed }
func (o *testOwner) setHealthy(v bool) { o.mu.Lock(); defer o.mu.Unlock(); o.healthy = v }
func (o *testOwner) started() bool     { o.mu.Lock(); defer o.mu.Unlock(); return o.didStart }
