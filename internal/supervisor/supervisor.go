package supervisor

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

// RuntimeOwner is supplied only by the future trusted-host launch composition.
// It keeps opaque direct handles; no caller can reconstruct a signal target
// from the evidence persisted in Manifest.
type RuntimeOwner interface {
	Start(context.Context, LaunchRequest) ([]ProcessIdentity, error)
	Snapshot() Snapshot
	Wait(context.Context) error
	Stop(context.Context) error
	Close(context.Context) error
}
type ProcessInspector interface {
	Supported() bool
	Observe(context.Context, int) (ProcessIdentity, error)
}

// DetachedLauncher is the concrete controller-side launch foundation. Its
// private starter seam exists solely to make fixed argv/environment ownership
// testable; production composition supplies an exec-backed starter in Task 5.
type DetachedLauncher struct {
	Executable string
	Inspector  ProcessInspector
	Start      func(context.Context, LaunchCommand) (ReleasedChild, error)
	Await      func(context.Context, *Client, Binding) error
}
type LaunchCommand struct {
	Path      string
	Args, Env []string
	Dir       string
}
type ReleasedChild interface{ Release() error }

func (l DetachedLauncher) Launch(ctx context.Context, request LaunchRequest) error {
	if l.Inspector == nil || !l.Inspector.Supported() {
		return fmt.Errorf("supervisor process identity is unsupported on this platform")
	}
	if err := validLaunchRequest(request); err != nil {
		return err
	}
	if !canonicalAbsolute(l.Executable) || l.Start == nil || l.Await == nil {
		return fmt.Errorf("fixed supervisor launcher dependencies are required")
	}
	path := filepath.Join(request.RuntimeDirectory, requestName)
	if err := writeLaunchRequest(path, request); err != nil {
		return err
	}
	child, err := l.Start(ctx, LaunchCommand{Path: l.Executable, Args: []string{"internal", "session-supervisor", path}, Env: []string{"PATH=/usr/bin:/bin", "LANG=C", "LC_ALL=C"}, Dir: request.RuntimeDirectory})
	if err != nil {
		_ = os.Remove(path)
		return err
	}
	if child == nil {
		_ = os.Remove(path)
		return fmt.Errorf("supervisor child is unavailable")
	}
	client := &Client{RuntimeDirectory: request.RuntimeDirectory, Inspector: l.Inspector, MaxSnapshotAge: time.Minute}
	if err := l.Await(ctx, client, request.Binding); err != nil {
		_ = child.Release()
		_ = os.Remove(path)
		return err
	}
	return nil
}

var _ Launcher = DetachedLauncher{}

// RunRequest is the fixed internal command entrypoint. Runtime composition is
// intentionally unavailable until Task 5 supplies admitted host/backend facts;
// refusing it is safer than synthesizing a handle from request data.
func RunRequest(ctx context.Context, requestPath string) error {
	return Run(ctx, requestPath, unavailableOwner{}, systemInspector{})
}

type unavailableOwner struct{}

func (unavailableOwner) Start(context.Context, LaunchRequest) ([]ProcessIdentity, error) {
	return nil, fmt.Errorf("supervisor runtime composition is unavailable")
}
func (unavailableOwner) Snapshot() Snapshot { return Snapshot{} }
func (unavailableOwner) Wait(context.Context) error {
	return fmt.Errorf("supervisor runtime composition is unavailable")
}
func (unavailableOwner) Stop(context.Context) error  { return nil }
func (unavailableOwner) Close(context.Context) error { return nil }

func prepare(ctx context.Context, requestPath string, owner RuntimeOwner, inspector ProcessInspector) (Manifest, error) {
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
	// Platform and request admission precede every runtime mutation/start.
	identity, err := inspector.Observe(ctx, os.Getpid())
	if err != nil || !identity.valid() {
		return Manifest{}, fmt.Errorf("observe supervisor process identity: %w", err)
	}
	children, err := owner.Start(ctx, request)
	if err != nil {
		return Manifest{}, err
	}
	if len(children) == 0 {
		return Manifest{}, fmt.Errorf("runtime owner returned no direct child evidence")
	}
	for _, child := range children {
		if !child.valid() {
			return Manifest{}, fmt.Errorf("runtime owner returned invalid direct child evidence")
		}
	}
	key, encoded, err := newControlKey()
	if err != nil {
		return Manifest{}, err
	}
	_ = key
	manifest := Manifest{Version: 1, Binding: request.Binding, RuntimeDirectory: request.RuntimeDirectory, SocketPath: filepath.Join(request.RuntimeDirectory, socketName), ControlKey: encoded, Supervisor: identity, Children: children, CreatedAt: time.Now().UTC()}
	if err := writeManifest(filepath.Join(request.RuntimeDirectory, manifestName), manifest); err != nil {
		_ = owner.Stop(context.Background())
		_ = owner.Close(context.Background())
		return Manifest{}, err
	}
	return manifest, nil
}

// Run is the detached supervisor lifetime. It closes its only socket and acts
// through held runtime handles once, whether backend exit, stop, or context
// cancellation ends the generation. It never scans process tables or signals
// a manifest PID.
func Run(ctx context.Context, requestPath string, owner RuntimeOwner, inspector ProcessInspector) error {
	if inspector == nil || !inspector.Supported() {
		return fmt.Errorf("supervisor process identity is unsupported on this platform")
	}
	request, err := readLaunchRequest(requestPath)
	if err != nil {
		return err
	}
	generationLock, err := acquireGenerationLock(request.RuntimeDirectory)
	if err != nil {
		return err
	}
	defer generationLock.Close()
	owned := &onceOwner{owner: owner}
	manifest, err := prepare(ctx, requestPath, owned, inspector)
	if err != nil {
		return err
	}
	key, err := decodeKey(manifest.ControlKey)
	if err != nil {
		return cleanup(owner, manifest)
	}
	listener, err := listenSocket(manifest.SocketPath)
	if err != nil {
		cleanupErr := cleanup(owned, manifest)
		if cleanupErr != nil {
			return fmt.Errorf("open control socket: %w; cleanup: %v", err, cleanupErr)
		}
		return fmt.Errorf("open control socket: %w", err)
	}
	controlCtx, cancel := context.WithCancel(context.Background())
	controlDone := make(chan error, 1)
	stopped := make(chan struct{}, 1)
	go func() { controlDone <- serveControl(controlCtx, listener, manifest, key, owned, stopped) }()
	waitDone := make(chan error, 1)
	go func() { waitDone <- owned.Wait(context.Background()) }()
	select {
	case <-ctx.Done():
	case <-waitDone:
	case <-controlDone:
	case <-stopped:
	}
	cancel()
	_ = listener.Close()
	select {
	case <-controlDone:
	case <-time.After(time.Second):
	}
	return cleanup(owned, manifest)
}

type onceOwner struct {
	owner     RuntimeOwner
	stopOnce  sync.Once
	stopErr   error
	closeOnce sync.Once
	closeErr  error
}

func (o *onceOwner) Start(ctx context.Context, r LaunchRequest) ([]ProcessIdentity, error) {
	return o.owner.Start(ctx, r)
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
func acquireGenerationLock(runtime string) (*os.File, error) {
	path := filepath.Join(runtime, "generation.lock")
	if info, err := os.Lstat(path); err == nil && (info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 || !ownedByCurrentUser(info)) {
		return nil, fmt.Errorf("generation lock is unsafe")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open generation lock: %w", err)
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 || !ownedByCurrentUser(info) {
		file.Close()
		return nil, fmt.Errorf("generation lock is unsafe")
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		file.Close()
		return nil, fmt.Errorf("acquire generation lock: %w", err)
	}
	return file, nil
}
func cleanup(owner RuntimeOwner, manifest Manifest) error {
	var once sync.Once
	var result error
	once.Do(func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := owner.Stop(stopCtx); err != nil {
			result = err
		}
		if err := owner.Close(stopCtx); err != nil && result == nil {
			result = err
		}
		for _, path := range []string{manifest.SocketPath, filepath.Join(manifest.RuntimeDirectory, manifestName), filepath.Join(manifest.RuntimeDirectory, requestName)} {
			if err := removeOwned(path); err != nil && result == nil {
				result = err
			}
		}
	})
	return result
}
func removeOwned(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if filepath.Base(path) == socketName {
		if info.Mode()&os.ModeSocket == 0 || !ownedByCurrentUser(info) || info.Mode().Perm() != 0o600 {
			return fmt.Errorf("refuse cleanup of unproven control socket")
		}
	} else if !privateRegular(path) {
		return fmt.Errorf("refuse cleanup of unproven manifest")
	}
	return os.Remove(path)
}

// Compile-time guard: the control socket is always a Unix endpoint, never a
// network service.
var _ *net.UnixListener
