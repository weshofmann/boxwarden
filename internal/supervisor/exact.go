package supervisor

import (
	"context"
	"errors"
	"fmt"
	"time"
)

type startupPolicy struct{ timeout, interval time.Duration }

var productionStartupPolicy = startupPolicy{timeout: 5 * time.Minute, interval: time.Second}

type ExactController struct {
	launcher   Launcher
	controller Controller
	policy     startupPolicy
}

func NewExactController(launcher Launcher, controller Controller) *ExactController {
	return &ExactController{launcher: launcher, controller: controller, policy: productionStartupPolicy}
}
func (c *ExactController) StartExact(ctx context.Context, request LaunchRequest) (Snapshot, error) {
	if c == nil || c.launcher == nil || c.controller == nil {
		return Snapshot{}, fmt.Errorf("exact supervisor dependencies are required")
	}
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	state, err := classifyExactGeneration(request)
	if err != nil {
		return Snapshot{}, err
	}
	if state != exactGenerationLive {
		if err := c.launcher.Launch(ctx, request); err != nil && !errors.Is(err, errGenerationAlreadyOwned) {
			return Snapshot{}, err
		}
	}
	return awaitSnapshot(ctx, request.Binding, c.policy, c.controller.Snapshot)
}
func awaitSnapshot(ctx context.Context, binding Binding, policy startupPolicy, snapshot func(context.Context, Binding) (Snapshot, error)) (Snapshot, error) {
	if policy.timeout <= 0 || policy.interval <= 0 {
		policy = productionStartupPolicy
	}
	ctx, cancel := context.WithTimeout(ctx, policy.timeout)
	defer cancel()
	ticker := time.NewTicker(policy.interval)
	defer ticker.Stop()
	var last error
	for {
		if err := ctx.Err(); err != nil {
			return Snapshot{}, errors.Join(err, last)
		}
		got, err := snapshot(ctx, binding)
		if err == nil {
			if got.Binding != binding {
				return Snapshot{}, fmt.Errorf("supervisor binding mismatch")
			}
			if snapshotReady(got) {
				return got, nil
			}
			last = fmt.Errorf("supervisor snapshot is not ready: %s", boundedDiagnostic(got.Diagnostic))
		} else {
			last = err
		}
		select {
		case <-ctx.Done():
			return Snapshot{}, errors.Join(ctx.Err(), last)
		case <-ticker.C:
		}
	}
}
func (c *ExactController) Snapshot(ctx context.Context, binding Binding) (Snapshot, error) {
	if c == nil || c.controller == nil {
		return Snapshot{}, fmt.Errorf("supervisor controller is unavailable")
	}
	return c.controller.Snapshot(ctx, binding)
}
func (c *ExactController) Stop(ctx context.Context, binding Binding) error {
	if c == nil || c.controller == nil {
		return fmt.Errorf("supervisor controller is unavailable")
	}
	return c.controller.Stop(ctx, binding)
}
