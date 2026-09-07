package supervisor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

// RuntimeOwner retains actual backend/serial handles. Start failure must undo
// partial acquisition before returning. After success, Wait reaps exactly once,
// and Stop targets only those retained handles. Wait also releases serial and
// other runtime resources before returning; the generation lock outlives it.
type RuntimeOwner interface {
	Start(context.Context, LaunchRequest) error
	Snapshot() Snapshot
	Stop(context.Context) error
	Wait(context.Context) error
}

// RunRequest deliberately refuses to start until a later slice supplies the
// authoritative configured-domain/session reload and actual runtime owner.
func RunRequest(ctx context.Context, path string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, err := readLaunchRequest(path); err != nil {
		return err
	}
	return fmt.Errorf("supervisor runtime composition is unavailable")
}

// Run owns one exact generation through a single stop/wait path. After owner
// startup cleanup or actual reap, it removes only the admitted outer namespace;
// the durable session record remains authoritative for same-generation retry.
func Run(ctx context.Context, path string, owner RuntimeOwner) error {
	if owner == nil {
		return fmt.Errorf("runtime owner is unavailable")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	request, err := readLaunchRequest(path)
	if err != nil {
		return err
	}
	later, err := admitGeneration(request)
	if err != nil {
		return err
	}
	lock, err := acquireGenerationLock(request)
	if err != nil {
		return err
	}
	defer lock.Close()
	if later {
		return fmt.Errorf("stale live artifacts require reconciliation")
	}
	if err := owner.Start(ctx, request); err != nil {
		return errors.Join(err, removeExactGeneration(request))
	}

	reaped := make(chan struct{})
	var waitErr error
	go func() { waitErr = owner.Wait(context.Background()); close(reaped) }()
	var stopOnce sync.Once
	var stopErr error
	stop := func() error {
		waitCtx, cancel := context.WithTimeout(context.Background(), lifecycleTimeout)
		defer cancel()
		stopOnce.Do(func() {
			stopErr = owner.Stop(waitCtx)
		})
		select {
		case <-reaped:
			return errors.Join(stopErr, waitErr)
		case <-waitCtx.Done():
			return errors.Join(stopErr, waitCtx.Err())
		}
	}
	finish := func(cause error) error {
		// A timeout is reported, but ownership is retained until actual reap.
		stopResult := stop()
		<-reaped
		return errors.Join(cause, stopResult, waitErr)
	}
	listener, err := listenSocket(filepath.Join(request.RuntimeDirectory, socketName))
	if err != nil {
		return errors.Join(finish(err), removeExactGeneration(request))
	}
	serveCtx, cancelServe := context.WithCancel(context.Background())
	serveDone := make(chan error, 1)
	go func() { serveDone <- serveControl(serveCtx, listener, request.Binding, owner, stop) }()
	var cause error
	serverExited := false
	select {
	case <-reaped:
	case cause = <-serveDone:
		serverExited = true
	case <-ctx.Done():
		cause = ctx.Err()
	}
	cancelServe()
	_ = listener.Close()
	// Accepted calls have a fixed deadline; join them before releasing the lock.
	if !serverExited {
		cause = errors.Join(cause, <-serveDone)
	}
	return errors.Join(finish(cause), removeExactGeneration(request))
}

type detachedLauncher struct{}

func NewDetachedLauncher() (Launcher, error) { return detachedLauncher{}, nil }

// The detached child claims its ordinary generation lock before runtime Start.
// Concurrent cooperating launches may create children, but only one can own
// the generation; every parent reaps its own child through one Wait goroutine.
func (detachedLauncher) Launch(ctx context.Context, request LaunchRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	path, _, err := publishOrAdmitRequest(request)
	if err != nil {
		return err
	}
	state, err := classifyExactGeneration(request)
	if err != nil {
		return err
	}
	client := &Client{RuntimeDirectory: request.RuntimeDirectory, MaxSnapshotAge: time.Minute}
	if state == exactGenerationLive {
		_, err := awaitSnapshot(ctx, request.Binding, productionStartupPolicy, client.Snapshot)
		return err
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(executable, "internal", "session-supervisor", path)
	cmd.Env = []string{"PATH=/usr/bin:/bin", "LANG=C", "LC_ALL=C"}
	cmd.Dir = request.RuntimeDirectory
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	childDone := make(chan struct{})
	var childErr error
	go func() { childErr = cmd.Wait(); close(childDone) }()
	readyCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		select {
		case <-childDone:
			cancel()
		case <-readyCtx.Done():
		}
	}()
	_, err = awaitSnapshot(readyCtx, request.Binding, productionStartupPolicy, client.Snapshot)
	if err == nil {
		return nil
	}
	select {
	case <-childDone:
		// A competing exact child may have won the generation lock.
		if state, stateErr := classifyExactGeneration(request); stateErr == nil && state == exactGenerationLive {
			_, err = awaitSnapshot(ctx, request.Binding, productionStartupPolicy, client.Snapshot)
			return err
		}
		return errors.Join(err, childErr)
	default:
		// A started supervisor owns cleanup: ask it to stop, never reconstruct or
		// signal backend process identities from disk. If control is unavailable,
		// retain the detached child for subsequent exact-generation reconciliation.
		stopCtx, stopCancel := context.WithTimeout(context.Background(), lifecycleTimeout+controlIOTimeout)
		stopErr := client.Stop(stopCtx, request.Binding)
		stopCancel()
		return errors.Join(err, stopErr)
	}
}
