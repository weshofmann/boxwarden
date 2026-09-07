package supervisor

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
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
	Start(context.Context, LaunchRequest) (RuntimeStartResult, error)
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
	Path           string
	Args, Env      []string
	Dir            string
	GenerationLock *os.File // fixed fd 3 in the detached child; no generic fd surface
}
type launchChild interface {
	release() error
	stop() error
	wait() error
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
	requestOnlyRecovery := generationIsRequestOnly(request.RuntimeDirectory)
	requestPath, firstPublication, err := publishOrAdmitRequest(request)
	if err != nil {
		return err
	}
	runtimeIdentity, err := capturePrivateDirectory(request.RuntimeDirectory)
	if err != nil {
		return err
	}
	admittedRequest, requestArtifact, err := admitLaunchRequest(requestPath)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(admittedRequest, request) {
		return errors.Join(fmt.Errorf("supervisor request changed during parent admission"), requestArtifact.close())
	}
	lockArtifact, err := admitBoundGenerationLock(filepath.Join(request.RuntimeDirectory, lockName), request)
	if err != nil {
		return errors.Join(fmt.Errorf("admit exact bound generation lock: %w", err), requestArtifact.close())
	}
	// A pre-existing, exact request-plus-lock-only namespace is a failed retry
	// foundation. Once this parent has re-admitted both identities and won the
	// flock below, it owns the same narrow cleanup obligation as request-only
	// recovery; later artifacts deliberately make this false and are preserved.
	resumableFoundation := generationIsExactBoundFoundation(request.RuntimeDirectory)
	if err := claimPublishedGenerationLock(lockArtifact); err != nil {
		return errors.Join(err, requestArtifact.close(), lockArtifact.close())
	}
	requestCleaned := false
	cleanupRequest := func() error {
		if requestCleaned {
			return nil
		}
		requestCleaned = true
		result := errors.Join(removeExact(lockArtifact.identity, false), lockArtifact.close())
		result = errors.Join(result, removeExact(requestArtifact.identity, false), requestArtifact.close())
		if firstPublication || requestOnlyRecovery || resumableFoundation {
			result = errors.Join(result, removeExactDirectory(runtimeIdentity))
		}
		return result
	}
	defer func() {
		if !requestCleaned {
			_ = requestArtifact.close()
			_ = lockArtifact.close()
		}
	}()
	if err := ctx.Err(); err != nil {
		return errors.Join(err, cleanupRequest())
	}
	child, err := l.deps.start(ctx, LaunchCommand{Path: executable, Args: []string{"internal", "session-supervisor", requestPath}, Env: []string{"PATH=/usr/bin:/bin", "LANG=C", "LC_ALL=C"}, Dir: request.RuntimeDirectory, GenerationLock: lockArtifact.file})
	if err != nil {
		return errors.Join(err, cleanupRequest())
	}
	if child == nil {
		return errors.Join(fmt.Errorf("supervisor child is unavailable"), cleanupRequest())
	}
	cleanupChild := func(cause error) error {
		reaper := &launchChildReaper{child: child, done: make(chan struct{})}
		stopErr := child.stop()
		reaper.start()
		waitCtx, cancel := context.WithTimeout(context.Background(), lifecycleDeadline())
		awaited := reaper.await(waitCtx)
		cancel()
		waitErr := awaited.err
		if !awaited.completed {
			// A timeout is diagnostic only. Retain this request and this launch
			// call until the sole held child has actually been reaped.
			<-reaper.done
			waitErr = errors.Join(waitErr, reaper.result())
		}
		return errors.Join(cause, stopErr, waitErr, cleanupRequest())
	}
	// exec.Cmd.Start has duplicated the fixed ExtraFiles entry into the child.
	// Close only the parent's copy now: the inherited open-file description
	// keeps the flock continuous across a parent crash while avoiding a second
	// long-lived parent owner.
	if err := lockArtifact.close(); err != nil {
		return cleanupChild(err)
	}
	client := &Client{RuntimeDirectory: request.RuntimeDirectory, Inspector: l.deps.inspector, MaxSnapshotAge: time.Minute}
	if err := l.deps.await(ctx, client, request.Binding); err != nil {
		return cleanupChild(err)
	}
	if err := child.release(); err != nil {
		return cleanupChild(err)
	}
	return errors.Join(requestArtifact.close(), lockArtifact.close())
}
func awaitAuthenticated(ctx context.Context, client *Client, binding Binding) error {
	if client == nil {
		return fmt.Errorf("supervisor client is unavailable")
	}
	return awaitAuthenticatedWithPolicy(ctx, binding, productionStartupPolicy, func(ctx context.Context) (Snapshot, error) {
		return client.Snapshot(ctx, binding)
	})
}

// awaitAuthenticatedWithPolicy is the detached launcher's private startup
// wait. It intentionally receives only the fixed binding and a snapshot
// function: it cannot acquire a lock, launch a process, or widen authority.
func awaitAuthenticatedWithPolicy(ctx context.Context, binding Binding, policy startupPolicy, snapshot func(context.Context) (Snapshot, error)) error {
	if snapshot == nil {
		return fmt.Errorf("supervisor snapshot function is unavailable")
	}
	if policy.timeout <= 0 || policy.interval <= 0 {
		policy = productionStartupPolicy
	}
	deadline := time.NewTimer(policy.timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(policy.interval)
	defer ticker.Stop()
	var last error
	for {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("await authenticated supervisor: %w", err)
		}
		got, err := snapshot(ctx)
		if err == nil {
			if got.Binding != binding {
				return fmt.Errorf("authenticated supervisor binding mismatch")
			}
			if snapshotReady(got) {
				return nil
			}
			last = snapshotPendingDiagnostic(got)
		} else {
			last = err
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("await authenticated supervisor: %w", ctx.Err())
		case <-deadline.C:
			return fmt.Errorf("await authenticated supervisor: %w", last)
		case <-ticker.C:
		}
	}
}

type exactChild struct{ cmd *exec.Cmd }

func startExactChild(ctx context.Context, command LaunchCommand) (launchChild, error) {
	if command.GenerationLock == nil {
		return nil, fmt.Errorf("inherited generation lock is unavailable")
	}
	cmd := exec.Command(command.Path, command.Args...)
	cmd.Env = append([]string(nil), command.Env...)
	cmd.Dir = command.Dir
	cmd.ExtraFiles = []*os.File{command.GenerationLock}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &exactChild{cmd: cmd}, nil
}

var errGenerationAlreadyOwned = errors.New("exact generation is already owned")

type generationAlreadyOwnedError struct{}

func (e generationAlreadyOwnedError) Error() string { return errGenerationAlreadyOwned.Error() }
func (e generationAlreadyOwnedError) Unwrap() error { return errGenerationAlreadyOwned }

func claimPublishedGenerationLock(lock *retainedPrivateFile) error {
	if lock == nil || lock.file == nil {
		return fmt.Errorf("published generation lock is unavailable")
	}
	if err := syscall.Flock(int(lock.file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			return generationAlreadyOwnedError{}
		}
		return fmt.Errorf("claim published generation lock: %w", err)
	}
	return nil
}
func (c *exactChild) release() error {
	if c == nil || c.cmd == nil || c.cmd.Process == nil {
		return fmt.Errorf("exact child unavailable")
	}
	return c.cmd.Process.Release()
}
func (c *exactChild) stop() error {
	if c == nil || c.cmd == nil || c.cmd.Process == nil {
		return nil
	}
	return c.cmd.Process.Kill()
}
func (c *exactChild) wait() error {
	if c == nil || c.cmd == nil {
		return nil
	}
	return c.cmd.Wait()
}

// RunRequest is deliberately unavailable until Task 5 supplies admitted
// host/backend facts. The launcher only treats an authenticated ready response
// as success; this refusal never fabricates an owner from request bytes.
func RunRequest(ctx context.Context, requestPath string) error {
	lock := os.NewFile(uintptr(3), "boxwarden-generation-lock")
	if lock == nil {
		return fmt.Errorf("inherited generation lock fd 3 is unavailable")
	}
	return runWithInheritedLock(ctx, requestPath, unavailableOwner{}, systemInspector{}, lock)
}

type unavailableOwner struct{}

func (unavailableOwner) Start(context.Context, LaunchRequest) (RuntimeStartResult, error) {
	return RuntimeStartResult{}, fmt.Errorf("supervisor runtime composition is unavailable")
}
func (unavailableOwner) Snapshot() Snapshot { return Snapshot{} }
func (unavailableOwner) Wait(context.Context) error {
	return fmt.Errorf("supervisor runtime composition is unavailable")
}
func (unavailableOwner) Stop(context.Context) error  { return nil }
func (unavailableOwner) Close(context.Context) error { return nil }

type lifecycleResources struct {
	runtime, request, manifest, socket, lock FileIdentity
	requestArtifact, manifestArtifact        *retainedPrivateFile
	lockArtifact                             *retainedPrivateFile
	root                                     *os.Root
}

func prepare(ctx context.Context, request LaunchRequest, owner RuntimeOwner, inspector ProcessInspector, runtimeIdentity FileIdentity) (Manifest, bool, error) {
	if inspector == nil || !inspector.Supported() {
		return Manifest{}, false, fmt.Errorf("supervisor process identity is unsupported on this platform")
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
		return Manifest{}, start.Owned, err
	}
	if !start.Owned {
		return Manifest{}, false, fmt.Errorf("runtime start succeeded without retained ownership")
	}
	if err := validStartEvidence(start.Evidence, request.RuntimeDirectory); err != nil {
		return Manifest{}, true, err
	}
	key, encoded, err := newControlKey()
	if err != nil {
		return Manifest{}, true, err
	}
	_ = key
	manifest := Manifest{Version: 1, Binding: request.Binding, RuntimeDirectory: request.RuntimeDirectory, SocketPath: filepath.Join(request.RuntimeDirectory, socketName), ControlKey: encoded, Evidence: RuntimeEvidence{Supervisor: supervisor, Children: start.Evidence.Children, Endpoints: start.Evidence.Endpoints, RuntimeDirectory: runtimeIdentity, Broker: start.Evidence.Broker}, CreatedAt: time.Now().UTC()}
	if err := writeManifest(filepath.Join(request.RuntimeDirectory, manifestName), manifest); err != nil {
		return Manifest{}, true, err
	}
	return manifest, true, nil
}

// Run owns one generation. Every outcome converges through one bounded
// stop/wait/close/identity-cleanup sequence; evidence is never used to signal.
func Run(ctx context.Context, requestPath string, owner RuntimeOwner, inspector ProcessInspector) error {
	return runWithInheritedLock(ctx, requestPath, owner, inspector, nil)
}

// runWithInheritedLock preserves the direct Run seam for deterministic tests.
// Production RunRequest always supplies fixed fd 3 and therefore cannot reopen
// or create a pathname lock.
func runWithInheritedLock(ctx context.Context, requestPath string, owner RuntimeOwner, inspector ProcessInspector, inheritedLock *os.File) error {
	if inspector == nil || !inspector.Supported() {
		return fmt.Errorf("supervisor process identity is unsupported on this platform")
	}
	request, requestArtifact, err := admitLaunchRequest(requestPath)
	if err != nil {
		return err
	}
	resources := lifecycleResources{request: requestArtifact.identity, requestArtifact: requestArtifact}
	defer func() { _ = resources.closeArtifacts() }()
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
	var lock *retainedPrivateFile
	if inheritedLock == nil {
		lock, err = acquirePublishedGenerationLockInRoot(resources.root, request.RuntimeDirectory, request)
	} else {
		lock, err = admitInheritedGenerationLockInRoot(resources.root, request.RuntimeDirectory, request, inheritedLock)
	}
	if err != nil {
		_ = resources.root.Close()
		return err
	}
	resources.lock, resources.lockArtifact = lock.identity, lock
	owned := &onceOwner{owner: owner}
	reaper := &ownerReaper{owner: owned, done: make(chan struct{})}
	manifest, started, err := prepare(ctx, request, owned, inspector, resources.runtime)
	if started {
		reaper.start()
	}
	if err != nil {
		return finishStarted(err, owned, reaper, started, resources)
	}
	admittedManifest, manifestArtifact, err := admitManifest(filepath.Join(request.RuntimeDirectory, manifestName))
	if err != nil {
		return finishStarted(err, owned, reaper, true, resources)
	}
	if !reflect.DeepEqual(admittedManifest, manifest) {
		_ = manifestArtifact.close()
		return finishStarted(fmt.Errorf("supervisor manifest changed during admission"), owned, reaper, true, resources)
	}
	manifest = admittedManifest
	resources.manifest, resources.manifestArtifact = manifestArtifact.identity, manifestArtifact
	key, err := decodeKey(manifest.ControlKey)
	if err != nil {
		return finishStarted(err, owned, reaper, true, resources)
	}
	listener, socketIdentity, err := listenSocket(manifest.SocketPath)
	if err != nil {
		return finishStarted(fmt.Errorf("open control socket: %w", err), owned, reaper, true, resources)
	}
	resources.socket = socketIdentity
	controlCtx, cancelControl := context.WithCancel(context.Background())
	defer cancelControl()
	stopped := make(chan struct{}, 1)
	stop := func() error {
		stopCtx, cancel := context.WithTimeout(context.Background(), lifecycleDeadline())
		defer cancel()
		err := owned.Stop(stopCtx)
		if err == nil {
			err = reaper.await(stopCtx).err
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
		// terminateAndCleanup joins the retained terminal result once.
		cause = nil
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
	return finishStarted(cause, owned, reaper, true, resources)
}

// finishStarted never abandons a live retained capability. A bounded timeout
// is observable to the control caller, but the supervisor remains the owner
// until the one reaper has a result and ordinary ordered cleanup can finish.
func finishStarted(cause error, owner RuntimeOwner, reaper *ownerReaper, started bool, resources lifecycleResources) error {
	return errors.Join(cause, terminateAndCleanup(owner, reaper, started, resources))
}

type onceOwner struct {
	owner     RuntimeOwner
	stopOnce  sync.Once
	stopErr   error
	closeOnce sync.Once
	closeErr  error
}

func (o *onceOwner) Start(ctx context.Context, request LaunchRequest) (RuntimeStartResult, error) {
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

// acquirePublishedGenerationLockInRoot is the detached child's exact lock
// claim. Unlike the legacy low-level flock helper, it never creates a file:
// admission proves the retained descriptor contains the lock bound to the
// already-admitted request before exclusive ownership is claimed. b2 must
// replace this reopen-and-flock step with parent descriptor handoff.
func acquirePublishedGenerationLockInRoot(root *os.Root, runtime string, request LaunchRequest) (*retainedPrivateFile, error) {
	if root == nil || request.RuntimeDirectory != runtime {
		return nil, fmt.Errorf("runtime root is unavailable")
	}
	path := filepath.Join(runtime, lockName)
	before, err := root.Lstat(lockName)
	if err != nil {
		return nil, fmt.Errorf("admit published generation lock: %w", err)
	}
	if before.Mode()&os.ModeSymlink != 0 || !before.Mode().IsRegular() || before.Mode().Perm() != 0o600 || !ownedByCurrentUser(before) {
		return nil, fmt.Errorf("published generation lock is unsafe")
	}
	file, err := root.OpenFile(lockName, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, fmt.Errorf("open published generation lock: %w", err)
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	identity, err := identityFor(path, info)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 || !ownedByCurrentUser(info) {
		_ = file.Close()
		return nil, fmt.Errorf("published generation lock is unsafe")
	}
	if lockAdmissionHook != nil {
		lockAdmissionHook()
	}
	after, err := root.Lstat(lockName)
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	afterIdentity, err := identityFor(path, after)
	if err != nil || !identity.matches(afterIdentity) || after.Mode()&os.ModeSymlink != 0 || !after.Mode().IsRegular() || after.Mode().Perm() != 0o600 || !ownedByCurrentUser(after) {
		_ = file.Close()
		return nil, fmt.Errorf("published generation lock was replaced during rooted admission")
	}
	lock, err := validateBoundGenerationLock(&retainedPrivateFile{identity: identity, file: file}, request)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(lock.file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = lock.close()
		return nil, fmt.Errorf("acquire published generation lock: %w", err)
	}
	return lock, nil
}

// admitInheritedGenerationLockInRoot accepts only the parent's fixed fd-3
// duplicate. It proves that descriptor and the rooted current pathname name
// the same private immutable bound lock; it does not reopen, create, or flock
// a path. The parent claim survives exec through the shared open description.
func admitInheritedGenerationLockInRoot(root *os.Root, runtime string, request LaunchRequest, file *os.File) (*retainedPrivateFile, error) {
	if root == nil || file == nil || request.RuntimeDirectory != runtime {
		return nil, fmt.Errorf("inherited generation lock is unavailable")
	}
	path := filepath.Join(runtime, lockName)
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 || !ownedByCurrentUser(info) {
		_ = file.Close()
		return nil, fmt.Errorf("inherited generation lock is unsafe")
	}
	identity, err := identityFor(path, info)
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	current, err := root.Lstat(lockName)
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	currentIdentity, err := identityFor(path, current)
	if err != nil || !identity.matches(currentIdentity) || current.Mode()&os.ModeSymlink != 0 || !current.Mode().IsRegular() || current.Mode().Perm() != 0o600 || !ownedByCurrentUser(current) {
		_ = file.Close()
		return nil, fmt.Errorf("inherited generation lock was replaced")
	}
	lock, err := validateBoundGenerationLock(&retainedPrivateFile{identity: identity, file: file}, request)
	if err != nil {
		return nil, err
	}
	if err := proveInheritedGenerationLockHeld(root, lock); err != nil {
		_ = lock.close()
		return nil, err
	}
	return lock, nil
}

// proveInheritedGenerationLockHeld uses a separate validation-only open. A
// successful nonblocking claim proves fd 3 was not carrying continuous
// ownership; release that probe immediately and reject. The probe descriptor
// is never returned or used as lock authority.
func proveInheritedGenerationLockHeld(root *os.Root, lock *retainedPrivateFile) error {
	probe, err := root.OpenFile(lockName, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	defer probe.Close()
	err = syscall.Flock(int(probe.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err == nil {
		_ = syscall.Flock(int(probe.Fd()), syscall.LOCK_UN)
		return fmt.Errorf("inherited generation lock is not held")
	}
	if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
		return nil
	}
	return fmt.Errorf("probe inherited generation lock: %w", err)
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

// launchChildReaper owns the only Wait for a failed detached launch. A launch
// retains both the exact command and request name until this result exists.
type launchChildReaper struct {
	child launchChild
	done  chan struct{}
	once  sync.Once
	mu    sync.Mutex
	err   error
}

type reapAwait struct {
	completed bool
	err       error
}

func (r *launchChildReaper) start() {
	r.once.Do(func() {
		go func() {
			err := r.child.wait()
			r.mu.Lock()
			r.err = err
			r.mu.Unlock()
			close(r.done)
		}()
	})
}
func (r *launchChildReaper) result() error { r.mu.Lock(); defer r.mu.Unlock(); return r.err }
func (r *launchChildReaper) await(ctx context.Context) reapAwait {
	return awaitCompletedReaper(ctx, r.done, r.result, func() error {
		return fmt.Errorf("supervisor child did not reap before cleanup deadline: %w", ctx.Err())
	})
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
func (r *ownerReaper) await(ctx context.Context) reapAwait {
	if r == nil {
		return reapAwait{completed: true, err: fmt.Errorf("owner reaper is unavailable")}
	}
	return awaitCompletedReaper(ctx, r.done, r.result, func() error {
		return fmt.Errorf("owner did not reap before cleanup deadline: %w", ctx.Err())
	})
}

// awaitCompletedReaper treats an already observable terminal result as more
// authoritative than a simultaneous cleanup deadline. The reaper owns the
// only Wait; a timeout remains diagnostic only when no result is observable.
func awaitCompletedReaper(ctx context.Context, done <-chan struct{}, result func() error, timeout func() error) reapAwait {
	select {
	case <-done:
		return reapAwait{completed: true, err: result()}
	default:
	}
	select {
	case <-done:
		return reapAwait{completed: true, err: result()}
	case <-ctx.Done():
	}
	select {
	case <-done:
		return reapAwait{completed: true, err: result()}
	default:
		return reapAwait{err: timeout()}
	}
}

func terminateAndCleanup(owner RuntimeOwner, reaper *ownerReaper, started bool, resources lifecycleResources) error {
	if !started {
		return cleanupNamespace(resources)
	}
	var result error
	stopCtx, cancel := context.WithTimeout(context.Background(), lifecycleDeadline())
	result = errors.Join(result, owner.Stop(stopCtx))
	cancel()
	waitCtx, cancelWait := context.WithTimeout(context.Background(), lifecycleDeadline())
	awaited := reaper.await(waitCtx)
	cancelWait()
	waitErr := awaited.err
	result = errors.Join(result, waitErr)
	if !awaited.completed {
		// The bounded wait is returned to the caller as diagnostic evidence, but
		// this supervisor must retain its generation until the sole reaper proves
		// that the held runtime is no longer live.
		<-reaper.done
		result = errors.Join(result, reaper.result())
	}
	closeCtx, cancel := context.WithTimeout(context.Background(), lifecycleDeadline())
	result = errors.Join(result, owner.Close(closeCtx))
	cancel()
	return errors.Join(result, cleanupNamespace(resources))
}
func cleanupNamespace(resources lifecycleResources) error {
	var result error
	for _, artifact := range []struct {
		identity FileIdentity
		retained *retainedPrivateFile
		allowNil bool
	}{
		{identity: resources.socket, allowNil: true},
		{identity: resources.manifest, retained: resources.manifestArtifact},
		{identity: resources.request, retained: resources.requestArtifact},
	} {
		if artifact.identity.Path != "" {
			result = errors.Join(result, removeExact(artifact.identity, artifact.allowNil))
		}
		if artifact.retained != nil {
			result = errors.Join(result, artifact.retained.close())
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
	if resources.lockArtifact != nil {
		result = errors.Join(result, resources.lockArtifact.close())
	}
	if resources.root != nil {
		result = errors.Join(result, resources.root.Close())
	}
	if resources.runtime.Path != "" {
		result = errors.Join(result, removeExactDirectory(resources.runtime))
	}
	return result
}

func (resources *lifecycleResources) closeArtifacts() error {
	if resources == nil {
		return nil
	}
	return errors.Join(resources.requestArtifact.close(), resources.manifestArtifact.close())
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
