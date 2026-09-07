package supervisor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// ExactController composes the existing launch and authenticated inspection
// seams into the only start capability exported to session lifecycle code.
// It intentionally owns no session record or backend authority.
type ExactController struct {
	launcher   Launcher
	controller Controller
}

func NewExactController(launcher Launcher, controller Controller) *ExactController {
	return &ExactController{launcher: launcher, controller: controller}
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
		return Snapshot{}, err
	}
	snapshot, err := c.controller.Snapshot(ctx, request.Binding)
	if err != nil {
		return Snapshot{}, fmt.Errorf("authenticate exact supervisor snapshot: %w", err)
	}
	if snapshot.Binding != request.Binding {
		return Snapshot{}, fmt.Errorf("authenticated supervisor binding mismatch")
	}
	return snapshot, nil
}

func (c *ExactController) reconcileLive(ctx context.Context, request LaunchRequest) (Snapshot, error) {
	deadline := time.NewTimer(100 * time.Millisecond)
	defer deadline.Stop()
	for {
		snapshot, err := c.controller.Snapshot(ctx, request.Binding)
		if err == nil {
			if snapshot.Binding != request.Binding {
				return Snapshot{}, fmt.Errorf("authenticated supervisor binding mismatch")
			}
			return snapshot, nil
		}
		select {
		case <-ctx.Done():
			return Snapshot{}, fmt.Errorf("reconcile authenticated exact live generation: %w", ctx.Err())
		case <-deadline.C:
			return Snapshot{}, fmt.Errorf("reconcile authenticated exact live generation: %w", err)
		case <-time.After(5 * time.Millisecond):
		}
	}
}

type exactGenerationState uint8

const (
	exactGenerationAbsent exactGenerationState = iota
	exactGenerationRequestOnly
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
	for _, entry := range entries {
		if err := validateLiveOuterEntry(request.RuntimeDirectory, entry); err != nil {
			return 0, err
		}
	}
	return exactGenerationLive, nil
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
