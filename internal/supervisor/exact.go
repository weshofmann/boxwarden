package supervisor

import (
	"context"
	"fmt"
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
	if request.SessionRecordName != "" && !validPart(request.SessionRecordName) {
		return Snapshot{}, fmt.Errorf("invalid canonical session record name")
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
