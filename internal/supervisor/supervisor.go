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
	"sync/atomic"
	"syscall"
	"time"
)

// Production allows a real owned Tart runtime a meaningful graceful-reap
// interval. Tests narrow this through the package-private seam below.
var lifecycleDeadlineNanos atomic.Int64

func init() { lifecycleDeadlineNanos.Store(int64(5 * time.Second)) }
func lifecycleDeadline() time.Duration {
	return time.Duration(lifecycleDeadlineNanos.Load())
}

// Admission hooks are test-only deterministic race seams. Production leaves
// them nil; they never create authority or alter the accepted same-UID
// post-validation race non-claim.
var runtimeAdmissionHook func()
var lockAdmissionHook func()

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
	if err := ctx.Err(); err != nil {
		return err
	}
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
	if err := ctx.Err(); err != nil {
		return errors.Join(err, cleanupRequest())
	}
	child, err := l.deps.start(ctx, LaunchCommand{Path: executable, Args: []string{"internal", "session-supervisor", requestPath}, Env: []string{"PATH=/usr/bin:/bin", "LANG=C", "LC_ALL=C"}, Dir: request.RuntimeDirectory})
	if err != nil {
		return errors.Join(err, cleanupRequest())
	}
	if child == nil {
		return errors.Join(fmt.Errorf("supervisor child is unavailable"), cleanupRequest())
	}
	cleanupChild := func(cause error) error {
		stopCtx, cancel := context.WithTimeout(context.Background(), lifecycleDeadline())
		defer cancel()
		return errors.Join(cause, child.stopReap(stopCtx), cleanupRequest())
	}
	client := &Client{RuntimeDirectory: request.RuntimeDirectory, Inspector: l.deps.inspector, MaxSnapshotAge: time.Minute}
	if err := l.deps.await(ctx, client, request.Binding); err != nil {
		return cleanupChild(err)
	}
	if err := child.release(); err != nil {
		return cleanupChild(err)
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

func startExactChild(ctx context.Context, command LaunchCommand) (launchChild, error) {
	cmd := exec.Command(command.Path, command.Args...)
	cmd.Env = append([]string(nil), command.Env...)
	cmd.Dir = command.Dir
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
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
	root                                     *os.Root
}

func prepare(ctx context.Context, requestPath string, owner RuntimeOwner, inspector ProcessInspector, runtimeIdentity FileIdentity) (Manifest, bool, error) {
	if inspector == nil || !inspector.Supported() {
		return Manifest{}, false, fmt.Errorf("supervisor process identity is unsupported on this platform")
	}
	request, err := readLaunchRequest(requestPath)
	if err != nil {
		return Manifest{}, false, err
	}
	if owner == nil {
		return Manifest{}, false, fmt.Errorf("runtime owner is unavailable")
	}
	supervisor, err := inspector.Observe(ctx, os.Getpid())
	if err != nil || !supervisor.valid() {
		return Manifest{}, false, fmt.Errorf("observe supervisor process identity: %w", err)
	}
	start, err := owner.Start(ctx, request)
	if err != nil {
		return Manifest{}, false, err
	}
	if err := validStartEvidence(start, request.RuntimeDirectory); err != nil {
		return Manifest{}, true, err
	}
	key, encoded, err := newControlKey()
	if err != nil {
		return Manifest{}, true, err
	}
	_ = key
	manifest := Manifest{Version: 1, Binding: request.Binding, RuntimeDirectory: request.RuntimeDirectory, SocketPath: filepath.Join(request.RuntimeDirectory, socketName), ControlKey: encoded, Evidence: RuntimeEvidence{Supervisor: supervisor, Children: start.Children, Endpoints: start.Endpoints, RuntimeDirectory: runtimeIdentity, Broker: start.Broker}, CreatedAt: time.Now().UTC()}
	if err := writeManifest(filepath.Join(request.RuntimeDirectory, manifestName), manifest); err != nil {
		return Manifest{}, true, err
	}
	return manifest, true, nil
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
	if resources.root, err = os.OpenRoot(request.RuntimeDirectory); err != nil {
		return err
	}
	if runtimeAdmissionHook != nil {
		runtimeAdmissionHook()
	}
	rootInfo, err := resources.root.Lstat(".")
	if err != nil {
		_ = resources.root.Close()
		return err
	}
	rootIdentity, err := identityFor(request.RuntimeDirectory, rootInfo)
	if err != nil || !resources.runtime.matches(rootIdentity) {
		_ = resources.root.Close()
		return fmt.Errorf("runtime directory changed during rooted admission")
	}
	currentRuntime, err := capturePrivateDirectory(request.RuntimeDirectory)
	if err != nil || !resources.runtime.matches(currentRuntime) {
		_ = resources.root.Close()
		return fmt.Errorf("runtime directory path was replaced during rooted admission")
	}
	if resources.request, err = capturePrivateRegular(requestPath); err != nil {
		_ = resources.root.Close()
		return err
	}
	lock, lockIdentity, err := acquireGenerationLockInRoot(resources.root, request.RuntimeDirectory)
	if err != nil {
		_ = resources.root.Close()
		return err
	}
	resources.lock, resources.lockHandle = lockIdentity, lock
	owned := &onceOwner{owner: owner}
	reaper := &ownerReaper{owner: owned, done: make(chan struct{})}
	manifest, started, err := prepare(ctx, requestPath, owned, inspector, resources.runtime)
	if started {
		reaper.start()
	}
	if err != nil {
		return finishStarted(err, owned, reaper, started, resources)
	}
	resources.manifest, err = capturePrivateRegular(filepath.Join(request.RuntimeDirectory, manifestName))
	if err != nil {
		return finishStarted(err, owned, reaper, true, resources)
	}
	key, err := decodeKey(manifest.ControlKey)
	if err != nil {
		return finishStarted(err, owned, reaper, true, resources)
	}
	listener, err := listenSocket(manifest.SocketPath)
	if err != nil {
		return finishStarted(fmt.Errorf("open control socket: %w", err), owned, reaper, true, resources)
	}
	resources.socket, err = captureSocket(manifest.SocketPath)
	if err != nil {
		_ = listener.Close()
		return finishStarted(err, owned, reaper, true, resources)
	}
	controlCtx, cancelControl := context.WithCancel(context.Background())
	defer cancelControl()
	stopped := make(chan struct{}, 1)
	stop := func() error {
		stopCtx, cancel := context.WithTimeout(context.Background(), lifecycleDeadline())
		defer cancel()
		err := owned.Stop(stopCtx)
		if err == nil {
			err = reaper.await(stopCtx)
		}
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
	var cause error
	select {
	case <-reaper.done:
		cause = reaper.result()
	case <-stopped:
	case cause = <-controlDone:
	case <-ctx.Done():
		cause = ctx.Err()
	}
	cancelControl()
	_ = listener.Close()
	select {
	case <-controlDone:
	case <-time.After(lifecycleDeadline()):
		cause = errors.Join(cause, fmt.Errorf("control server did not stop"))
	}
	return finishStarted(cause, owned, reaper, true, resources, stop)
}

// finishStarted never abandons a live retained capability. A bounded timeout
// is observable to the control caller, but the supervisor remains the owner
// until the one reaper has a result and ordinary ordered cleanup can finish.
func finishStarted(cause error, owner RuntimeOwner, reaper *ownerReaper, started bool, resources lifecycleResources, stopFns ...func() error) error {
	cleanup := terminateAndCleanup(owner, reaper, started, resources, stopFns...)
	if cleanup != nil && started {
		select {
		case <-reaper.done:
			cleanup = errors.Join(cleanup, terminateAndCleanup(owner, reaper, started, resources, stopFns...))
		default:
			<-reaper.done
			cleanup = errors.Join(cleanup, terminateAndCleanup(owner, reaper, started, resources, stopFns...))
		}
	}
	return errors.Join(cause, cleanup)
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
	root, err := os.OpenRoot(runtime)
	if err != nil {
		return nil, FileIdentity{}, err
	}
	defer root.Close()
	return acquireGenerationLockInRoot(root, runtime)
}
func acquireGenerationLockInRoot(root *os.Root, runtime string) (*os.File, FileIdentity, error) {
	if root == nil {
		return nil, FileIdentity{}, fmt.Errorf("runtime root is unavailable")
	}
	path := filepath.Join(runtime, lockName)
	if info, err := root.Lstat(lockName); err == nil && (info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 || !ownedByCurrentUser(info)) {
		return nil, FileIdentity{}, fmt.Errorf("generation lock is unsafe")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, FileIdentity{}, err
	}
	file, err := root.OpenFile(lockName, os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0o600)
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
	if lockAdmissionHook != nil {
		lockAdmissionHook()
	}
	current, err := root.Lstat(lockName)
	if err != nil {
		_ = file.Close()
		return nil, FileIdentity{}, err
	}
	currentIdentity, err := identityFor(path, current)
	if err != nil || !identity.matches(currentIdentity) || current.Mode()&os.ModeSymlink != 0 || !current.Mode().IsRegular() || current.Mode().Perm() != 0o600 || !ownedByCurrentUser(current) {
		_ = file.Close()
		return nil, FileIdentity{}, fmt.Errorf("generation lock was replaced during rooted admission")
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

type ownerReaper struct {
	owner RuntimeOwner
	done  chan struct{}
	once  sync.Once
	mu    sync.Mutex
	err   error
}

func (r *ownerReaper) start() {
	r.once.Do(func() {
		go func() {
			err := r.owner.Wait(context.Background())
			r.mu.Lock()
			r.err = err
			r.mu.Unlock()
			close(r.done)
		}()
	})
}
func (r *ownerReaper) result() error { r.mu.Lock(); defer r.mu.Unlock(); return r.err }
func (r *ownerReaper) await(ctx context.Context) error {
	if r == nil {
		return fmt.Errorf("owner reaper is unavailable")
	}
	select {
	case <-r.done:
		return r.result()
	case <-ctx.Done():
		return fmt.Errorf("owner did not reap before cleanup deadline: %w", ctx.Err())
	}
}

func terminateAndCleanup(owner RuntimeOwner, reaper *ownerReaper, started bool, resources lifecycleResources, stopFns ...func() error) error {
	var result error
	if !started {
		return cleanupNamespace(resources)
	}
	if len(stopFns) > 0 {
		result = errors.Join(result, stopFns[0]())
	} else {
		stopCtx, cancel := context.WithTimeout(context.Background(), lifecycleDeadline())
		result = errors.Join(result, owner.Stop(stopCtx))
		cancel()
	}
	if result != nil {
		return result
	}
	waitCtx, cancelWait := context.WithTimeout(context.Background(), lifecycleDeadline())
	result = errors.Join(result, reaper.await(waitCtx))
	cancelWait()
	if result != nil {
		return result
	}
	closeCtx, cancel := context.WithTimeout(context.Background(), lifecycleDeadline())
	result = errors.Join(result, owner.Close(closeCtx))
	cancel()
	return errors.Join(result, cleanupNamespace(resources))
}
func cleanupNamespace(resources lifecycleResources) error {
	var result error
	for _, identity := range []FileIdentity{resources.socket, resources.manifest, resources.request} {
		if identity.Path != "" {
			result = errors.Join(result, removeExact(identity, identity.Path == resources.socket.Path))
		}
	}
	if resources.lock.Path != "" {
		if resources.root != nil {
			info, err := resources.root.Lstat(lockName)
			if err != nil {
				result = errors.Join(result, fmt.Errorf("preserve lock replacement during cleanup: %w", err))
			} else if current, err := identityFor(resources.lock.Path, info); err != nil || !resources.lock.matches(current) || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 || !ownedByCurrentUser(info) {
				result = errors.Join(result, fmt.Errorf("preserve lock replacement during cleanup"))
			} else {
				result = errors.Join(result, resources.root.Remove(lockName))
			}
		} else {
			result = errors.Join(result, removeExact(resources.lock, false))
		}
	}
	if resources.lockHandle != nil {
		result = errors.Join(result, resources.lockHandle.Close())
	}
	if resources.root != nil {
		result = errors.Join(result, resources.root.Close())
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
