package supervisor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// ExactController composes the existing launch and authenticated inspection
// seams into the only start capability exported to session lifecycle code.
// It intentionally owns no session record or backend authority.
type ExactController struct {
	launcher   Launcher
	controller Controller
	policy     startupPolicy
}
type startupPolicy struct{ timeout, interval time.Duration }

var productionStartupPolicy = startupPolicy{timeout: 5 * time.Minute, interval: time.Second}

func NewExactController(launcher Launcher, controller Controller) *ExactController {
	return &ExactController{launcher: launcher, controller: controller, policy: productionStartupPolicy}
}
func newExactController(launcher Launcher, controller Controller, policy startupPolicy) *ExactController {
	return &ExactController{launcher: launcher, controller: controller, policy: policy}
}

func (c *ExactController) StartExact(ctx context.Context, request LaunchRequest) (Snapshot, error) {
	if c == nil || c.launcher == nil || c.controller == nil {
		return Snapshot{}, fmt.Errorf("exact supervisor dependencies are required")
	}
	if err := validLaunchRequest(request); err != nil {
		return Snapshot{}, err
	}
	state, err := classifyExactGeneration(request)
	if err != nil {
		return Snapshot{}, err
	}
	if state == exactGenerationLive {
		return c.reconcileLive(ctx, request)
	}
	if err := c.launcher.Launch(ctx, request); err != nil {
		if errors.Is(err, errGenerationAlreadyOwned) {
			return c.reconcileLive(ctx, request)
		}
		return Snapshot{}, err
	}
	return c.reconcileLive(ctx, request)
}

func (c *ExactController) reconcileLive(ctx context.Context, request LaunchRequest) (Snapshot, error) {
	policy := c.policy
	if policy.timeout <= 0 || policy.interval <= 0 {
		policy = productionStartupPolicy
	}
	deadline := time.NewTimer(policy.timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(policy.interval)
	defer ticker.Stop()
	var last error
	for {
		snapshot, err := c.controller.Snapshot(ctx, request.Binding)
		if err == nil {
			if snapshot.Binding != request.Binding {
				return Snapshot{}, fmt.Errorf("authenticated supervisor binding mismatch")
			}
			if snapshotReady(snapshot) {
				return snapshot, nil
			}
			last = fmt.Errorf("authenticated supervisor snapshot is not ready")
		} else {
			last = err
		}
		select {
		case <-ctx.Done():
			return Snapshot{}, fmt.Errorf("reconcile authenticated exact live generation: %w", ctx.Err())
		case <-deadline.C:
			return Snapshot{}, fmt.Errorf("reconcile authenticated exact live generation: %w", last)
		case <-ticker.C:
		}
	}
}

type exactGenerationState uint8

const (
	exactGenerationAbsent exactGenerationState = iota
	exactGenerationRequestOnly
	exactGenerationResumable
	exactGenerationLive
)

// classifyExactGeneration rejects ambiguous state without mutation. A live
// namespace is never adopted from files alone: StartExact must authenticate
// the retained owner through Controller before returning it to session.
func classifyExactGeneration(request LaunchRequest) (exactGenerationState, error) {
	info, err := os.Lstat(request.RuntimeDirectory)
	if os.IsNotExist(err) {
		return exactGenerationAbsent, nil
	}
	if err != nil {
		return 0, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || !privateDirectory(request.RuntimeDirectory) {
		return 0, fmt.Errorf("exact generation is not an owner-private directory")
	}
	entries, err := os.ReadDir(request.RuntimeDirectory)
	if err != nil {
		return 0, err
	}
	if len(entries) == 0 {
		return 0, fmt.Errorf("exact generation is empty drift")
	}
	requestPath := filepath.Join(request.RuntimeDirectory, requestName)
	got, retained, err := admitLaunchRequest(requestPath)
	if retained != nil {
		defer retained.close()
	}
	if err != nil || !launchRequestsEqual(got, request) {
		return 0, fmt.Errorf("exact generation request is missing or foreign: %w", err)
	}
	if len(entries) == 1 && entries[0].Name() == requestName && !entries[0].IsDir() {
		return exactGenerationRequestOnly, nil
	}
	lock, err := admitBoundGenerationLock(filepath.Join(request.RuntimeDirectory, lockName), request)
	if lock != nil {
		defer lock.close()
	}
	if err != nil {
		return 0, fmt.Errorf("exact generation lock is missing or foreign: %w", err)
	}
	later := false
	for _, entry := range entries {
		if err := validateLiveOuterEntry(request.RuntimeDirectory, entry); err != nil {
			return 0, err
		}
		if entry.Name() != requestName && entry.Name() != lockName {
			later = true
		}
	}
	if err := syscall.Flock(int(lock.file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err == nil {
		_ = syscall.Flock(int(lock.file.Fd()), syscall.LOCK_UN)
		if later {
			return 0, fmt.Errorf("exact generation has stale unheld live artifacts")
		}
		return exactGenerationResumable, nil
	} else if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
		return exactGenerationLive, nil
	} else {
		return 0, err
	}
}

func validateLiveOuterEntry(runtime string, entry os.DirEntry) error {
	name := entry.Name()
	if name == requestName {
		return nil
	}
	info, err := os.Lstat(filepath.Join(runtime, name))
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !ownedByCurrentUser(info) {
		return fmt.Errorf("exact generation has unsafe live entry %q", name)
	}
	switch name {
	case lockName, manifestName:
		if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
			return fmt.Errorf("exact generation has malformed live entry %q", name)
		}
	case socketName:
		if info.Mode()&os.ModeSocket == 0 || info.Mode().Perm() != 0o600 {
			return fmt.Errorf("exact generation has malformed live entry %q", name)
		}
	case "serial":
		if !info.IsDir() || info.Mode().Perm() != 0o700 {
			return fmt.Errorf("exact generation has malformed serial entry")
		}
	case "client", "known_hosts":
		if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
			return fmt.Errorf("exact generation has malformed credential %q", name)
		}
	case "client.pub", "client-cert.pub":
		if !info.Mode().IsRegular() || info.Mode().Perm() != 0o644 {
			return fmt.Errorf("exact generation has malformed credential %q", name)
		}
	default:
		return fmt.Errorf("exact generation has unexpected live entry %q", name)
	}
	return nil
}

func (c *ExactController) Snapshot(ctx context.Context, binding Binding) (Snapshot, error) {
	if c == nil || c.controller == nil {
		return Snapshot{}, fmt.Errorf("exact supervisor dependencies are required")
	}
	return c.controller.Snapshot(ctx, binding)
}

func (c *ExactController) Stop(ctx context.Context, binding Binding) error {
	if c == nil || c.controller == nil {
		return fmt.Errorf("exact supervisor dependencies are required")
	}
	return c.controller.Stop(ctx, binding)
}
