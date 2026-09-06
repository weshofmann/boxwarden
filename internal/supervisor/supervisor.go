package supervisor

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

const lifecycleDeadline = 250 * time.Millisecond

// RuntimeOwner is supplied only by the future trusted-host launch composition.
// Its retained capabilities, rather than persisted evidence, are the only
// objects that may stop, wait for, or close the direct children. Task 5 must
// derive RuntimeStartEvidence read-only from its held backend and serial-runtime
// capabilities; it must not reconstruct a control target from this evidence.
type RuntimeOwner interface {
	Start(context.Context, LaunchRequest) (RuntimeStartEvidence, error)
	Snapshot() Snapshot
	Wait(context.Context) error
	Stop(context.Context) error
	Close(context.Context) error
}
type ProcessInspector interface {
	Supported() bool
	Observe(context.Context, int) (ProcessIdentity, error)
}

type LaunchCommand struct {
	Path      string
	Args, Env []string
	Dir       string
}
type launchChild interface {
	release() error
	stopReap(context.Context) error
}
type launcherDeps struct {
	executable func() (string, error)
	inspector  ProcessInspector
	start      func(context.Context, LaunchCommand) (launchChild, error)
	await      func(context.Context, *Client, Binding) error
}
type detachedLauncher struct{ deps launcherDeps }

// NewDetachedLauncher has no caller-controlled executable, process, or
// authentication dependencies. It derives the exact current Boxwarden binary.
func NewDetachedLauncher() (Launcher, error) {
	return newDetachedLauncher(launcherDeps{executable: os.Executable, inspector: systemInspector{}, start: startExactChild, await: awaitAuthenticated}), nil
}
func newDetachedLauncher(deps launcherDeps) Launcher { return detachedLauncher{deps: deps} }
func (l detachedLauncher) Launch(ctx context.Context, request LaunchRequest) error {
	// Unsupported platforms fail before creating a request or process.
	if l.deps.inspector == nil || !l.deps.inspector.Supported() {
		return fmt.Errorf("supervisor process identity is unsupported on this platform")
	}
	if err := validLaunchRequest(request); err != nil {
		return err
	}
	if l.deps.executable == nil || l.deps.start == nil || l.deps.await == nil {
		return fmt.Errorf("fixed supervisor launcher dependencies are required")
	}
	executable, err := l.deps.executable()
	if err != nil || !canonicalAbsolute(executable) {
		return fmt.Errorf("resolve exact boxwarden executable: %w", err)
	}
	requestPath := filepath.Join(request.RuntimeDirectory, requestName)
	if err := writeLaunchRequest(requestPath, request); err != nil {
		return err
	}
	requestIdentity, err := capturePrivateRegular(requestPath)
	if err != nil {
		return err
	}
	cleanupRequest := func() error { return removeExact(requestIdentity, false) }
	child, err := l.deps.start(ctx, LaunchCommand{Path: executable, Args: []string{"internal", "session-supervisor", requestPath}, Env: []string{"PATH=/usr/bin:/bin", "LANG=C", "LC_ALL=C"}, Dir: request.RuntimeDirectory})
	if err != nil {
		_ = cleanupRequest()
		return err
	}
	if child == nil {
		_ = cleanupRequest()
		return fmt.Errorf("supervisor child is unavailable")
	}
	client := &Client{RuntimeDirectory: request.RuntimeDirectory, Inspector: l.deps.inspector, MaxSnapshotAge: time.Minute}
	if err := l.deps.await(ctx, client, request.Binding); err != nil {
		stopCtx, cancel := context.WithTimeout(context.Background(), lifecycleDeadline)
		defer cancel()
		stopErr := child.stopReap(stopCtx)
		removeErr := cleanupRequest()
		return errors.Join(err, stopErr, removeErr)
	}
	if err := child.release(); err != nil {
		return err
	}
	return nil
}
func awaitAuthenticated(ctx context.Context, client *Client, binding Binding) error {
	snapshot, err := client.Snapshot(ctx, binding)
	if err != nil {
		return err
	}
	if !snapshotReady(snapshot) {
		return fmt.Errorf("supervisor authenticated snapshot is not ready")
	}
	return nil
}

type exactChild struct{ cmd *exec.Cmd }

func startExactChild(_ context.Context, command LaunchCommand) (launchChild, error) {
	cmd := exec.Command(command.Path, command.Args...)
	cmd.Env = append([]string(nil), command.Env...)
	cmd.Dir = command.Dir
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &exactChild{cmd: cmd}, nil
}
func (c *exactChild) release() error {
	if c == nil || c.cmd == nil || c.cmd.Process == nil {
		return fmt.Errorf("exact child unavailable")
	}
	return c.cmd.Process.Release()
}
func (c *exactChild) stopReap(ctx context.Context) error {
	if c == nil || c.cmd == nil || c.cmd.Process == nil {
		return nil
	}
	_ = c.cmd.Process.Kill()
	done := make(chan error, 1)
	go func() { done <- c.cmd.Wait() }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// RunRequest is deliberately unavailable until Task 5 supplies admitted
// host/backend facts. The launcher only treats an authenticated ready response
// as success; this refusal never fabricates an owner from request bytes.
func RunRequest(ctx context.Context, requestPath string) error {
	return Run(ctx, requestPath, unavailableOwner{}, systemInspector{})
}

type unavailableOwner struct{}

func (unavailableOwner) Start(context.Context, LaunchRequest) (RuntimeStartEvidence, error) {
	return RuntimeStartEvidence{}, fmt.Errorf("supervisor runtime composition is unavailable")
}
func (unavailableOwner) Snapshot() Snapshot { return Snapshot{} }
func (unavailableOwner) Wait(context.Context) error {
	return fmt.Errorf("supervisor runtime composition is unavailable")
}
func (unavailableOwner) Stop(context.Context) error  { return nil }
func (unavailableOwner) Close(context.Context) error { return nil }

type lifecycleResources struct {
	runtime, request, manifest, socket, lock FileIdentity
	lockHandle                               *os.File
}

func prepare(ctx context.Context, requestPath string, owner RuntimeOwner, inspector ProcessInspector, runtimeIdentity FileIdentity) (Manifest, error) {
	if inspector == nil || !inspector.Supported() {
		return Manifest{}, fmt.Errorf("supervisor process identity is unsupported on this platform")
	}
	request, err := readLaunchRequest(requestPath)
	if err != nil {
		return Manifest{}, err
	}
	if owner == nil {
		return Manifest{}, fmt.Errorf("runtime owner is unavailable")
	}
	supervisor, err := inspector.Observe(ctx, os.Getpid())
	if err != nil || !supervisor.valid() {
		return Manifest{}, fmt.Errorf("observe supervisor process identity: %w", err)
	}
	start, err := owner.Start(ctx, request)
	if err != nil {
		return Manifest{}, err
	}
	if err := validStartEvidence(start); err != nil {
		return Manifest{}, err
	}
	key, encoded, err := newControlKey()
	if err != nil {
		return Manifest{}, err
	}
	_ = key
	manifest := Manifest{Version: 1, Binding: request.Binding, RuntimeDirectory: request.RuntimeDirectory, SocketPath: filepath.Join(request.RuntimeDirectory, socketName), ControlKey: encoded, Evidence: RuntimeEvidence{Supervisor: supervisor, Children: start.Children, Endpoints: start.Endpoints, RuntimeDirectory: runtimeIdentity, Broker: start.Broker}, CreatedAt: time.Now().UTC()}
	if err := writeManifest(filepath.Join(request.RuntimeDirectory, manifestName), manifest); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

// Run owns one generation. Every outcome converges through one bounded
// stop/wait/close/identity-cleanup sequence; evidence is never used to signal.
func Run(ctx context.Context, requestPath string, owner RuntimeOwner, inspector ProcessInspector) error {
	if inspector == nil || !inspector.Supported() {
		return fmt.Errorf("supervisor process identity is unsupported on this platform")
	}
	request, err := readLaunchRequest(requestPath)
	if err != nil {
		return err
	}
	resources := lifecycleResources{}
	if resources.runtime, err = capturePrivateDirectory(request.RuntimeDirectory); err != nil {
		return err
	}
	if resources.request, err = capturePrivateRegular(requestPath); err != nil {
		return err
	}
	lock, lockIdentity, err := acquireGenerationLock(request.RuntimeDirectory)
	if err != nil {
		return err
	}
	resources.lock, resources.lockHandle = lockIdentity, lock
	owned := &onceOwner{owner: owner}
	manifest, err := prepare(ctx, requestPath, owned, inspector, resources.runtime)
	if err != nil {
		return errors.Join(err, terminateAndCleanup(owned, nil, resources))
	}
	resources.manifest, err = capturePrivateRegular(filepath.Join(request.RuntimeDirectory, manifestName))
	if err != nil {
		return errors.Join(err, terminateAndCleanup(owned, nil, resources))
	}
	key, err := decodeKey(manifest.ControlKey)
	if err != nil {
		return errors.Join(err, terminateAndCleanup(owned, nil, resources))
	}
	listener, err := listenSocket(manifest.SocketPath)
	if err != nil {
		return errors.Join(fmt.Errorf("open control socket: %w", err), terminateAndCleanup(owned, nil, resources))
	}
	resources.socket, err = captureSocket(manifest.SocketPath)
	if err != nil {
		_ = listener.Close()
		return errors.Join(err, terminateAndCleanup(owned, nil, resources))
	}
	controlCtx, cancelControl := context.WithCancel(context.Background())
	defer cancelControl()
	stopped := make(chan struct{}, 1)
	stop := func() error {
		stopCtx, cancel := context.WithTimeout(context.Background(), lifecycleDeadline)
		defer cancel()
		err := owned.Stop(stopCtx)
		if err == nil {
			select {
			case stopped <- struct{}{}:
			default:
			}
		}
		return err
	}
	controlDone := make(chan error, 1)
	go func() { controlDone <- serveControl(controlCtx, listener, manifest, key, owned, stop) }()
	waitCtx, cancelWait := context.WithCancel(context.Background())
	defer cancelWait()
	waitDone := make(chan error, 1)
	go func() { waitDone <- owned.Wait(waitCtx) }()
	var cause error
	waitObserved := false
	select {
	case cause = <-waitDone:
		waitObserved = true
	case <-stopped:
	case cause = <-controlDone:
	case <-ctx.Done():
		cause = ctx.Err()
	}
	cancelControl()
	_ = listener.Close()
	select {
	case <-controlDone:
	case <-time.After(lifecycleDeadline):
		cause = errors.Join(cause, fmt.Errorf("control server did not stop"))
	}
	if waitObserved {
		waitDone = nil
	}
	return errors.Join(cause, terminateAndCleanup(owned, waitDone, resources, stop))
}

type onceOwner struct {
	owner     RuntimeOwner
	stopOnce  sync.Once
	stopErr   error
	closeOnce sync.Once
	closeErr  error
}

func (o *onceOwner) Start(ctx context.Context, request LaunchRequest) (RuntimeStartEvidence, error) {
	return o.owner.Start(ctx, request)
}
func (o *onceOwner) Snapshot() Snapshot             { return o.owner.Snapshot() }
func (o *onceOwner) Wait(ctx context.Context) error { return o.owner.Wait(ctx) }
func (o *onceOwner) Stop(ctx context.Context) error {
	o.stopOnce.Do(func() { o.stopErr = o.owner.Stop(ctx) })
	return o.stopErr
}
func (o *onceOwner) Close(ctx context.Context) error {
	o.closeOnce.Do(func() { o.closeErr = o.owner.Close(ctx) })
	return o.closeErr
}

func acquireGenerationLock(runtime string) (*os.File, FileIdentity, error) {
	path := filepath.Join(runtime, lockName)
	if info, err := os.Lstat(path); err == nil && (info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 || !ownedByCurrentUser(info)) {
		return nil, FileIdentity{}, fmt.Errorf("generation lock is unsafe")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, FileIdentity{}, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, FileIdentity{}, fmt.Errorf("open generation lock: %w", err)
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 || !ownedByCurrentUser(info) {
		_ = file.Close()
		return nil, FileIdentity{}, fmt.Errorf("generation lock is unsafe")
	}
	identity, err := identityFor(path, info)
	if err != nil {
		_ = file.Close()
		return nil, FileIdentity{}, err
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = file.Close()
		return nil, FileIdentity{}, fmt.Errorf("acquire generation lock: %w", err)
	}
	return file, identity, nil
}
func captureSocket(path string) (FileIdentity, error) {
	identity, info, err := captureIdentity(path)
	if err != nil || info.Mode()&os.ModeSocket == 0 || info.Mode().Perm() != 0o600 || !ownedByCurrentUser(info) {
		return FileIdentity{}, fmt.Errorf("owner-private socket identity unavailable")
	}
	return identity, nil
}
func terminateAndCleanup(owner RuntimeOwner, waitDone <-chan error, resources lifecycleResources, stopFns ...func() error) error {
	var result error
	if len(stopFns) > 0 {
		result = errors.Join(result, stopFns[0]())
	} else {
		stopCtx, cancel := context.WithTimeout(context.Background(), lifecycleDeadline)
		result = errors.Join(result, owner.Stop(stopCtx))
		cancel()
	}
	if waitDone != nil {
		select {
		case err := <-waitDone:
			result = errors.Join(result, err)
		case <-time.After(lifecycleDeadline):
			result = errors.Join(result, fmt.Errorf("owner did not reap before cleanup deadline"))
		}
	}
	closeCtx, cancel := context.WithTimeout(context.Background(), lifecycleDeadline)
	result = errors.Join(result, owner.Close(closeCtx))
	cancel()
	for _, identity := range []FileIdentity{resources.socket, resources.manifest, resources.request, resources.lock} {
		if identity.Path != "" {
			result = errors.Join(result, removeExact(identity, identity.Path == resources.socket.Path))
		}
	}
	if resources.lockHandle != nil {
		result = errors.Join(result, resources.lockHandle.Close())
	}
	if resources.runtime.Path != "" {
		result = errors.Join(result, removeExactDirectory(resources.runtime))
	}
	return result
}
func removeExact(identity FileIdentity, allowMissing bool) error {
	err := identityStillMatches(identity, func(info os.FileInfo) bool { return ownedByCurrentUser(info) })
	if errors.Is(err, os.ErrNotExist) && allowMissing {
		return nil
	}
	if err != nil {
		return fmt.Errorf("preserve replacement during cleanup: %w", err)
	}
	if err := os.Remove(identity.Path); err != nil {
		return err
	}
	return nil
}
func removeExactDirectory(identity FileIdentity) error {
	if err := identityStillMatches(identity, func(info os.FileInfo) bool {
		return info.IsDir() && info.Mode().Perm() == 0o700 && ownedByCurrentUser(info)
	}); err != nil {
		return fmt.Errorf("preserve runtime replacement during cleanup: %w", err)
	}
	return os.Remove(identity.Path)
}

var _ *net.UnixListener
