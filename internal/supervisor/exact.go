package supervisor

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"time"
)

type startupPolicy struct{ timeout, interval time.Duration }

var productionStartupPolicy = startupPolicy{timeout: 5 * time.Minute, interval: time.Second}

var errExactGenerationTransition = errors.New("exact live generation transitioned")

type exactStartController struct {
	launcher   Launcher
	controller Controller
	policy     startupPolicy
}

// ExactController is the session-facing supervisor authority. Every control
// client is derived from one configured runtime root and the exact binding;
// callers cannot redirect a binding to another runtime namespace.
type ExactController struct {
	runtimeRoot string
	launcher    Launcher
	policy      startupPolicy
}

func NewExactController(runtimeRoot string, launcher Launcher) (*ExactController, error) {
	if !canonicalAbsolute(runtimeRoot) || launcher == nil {
		return nil, fmt.Errorf("root supervisor dependencies are required")
	}
	return &ExactController{runtimeRoot: runtimeRoot, launcher: launcher, policy: productionStartupPolicy}, nil
}

func (c *ExactController) StartExact(ctx context.Context, request LaunchRequest) (Snapshot, error) {
	directory, err := c.runtimeDirectory(request.Binding)
	if err != nil {
		return Snapshot{}, err
	}
	if request.RuntimeDirectory != directory {
		return Snapshot{}, fmt.Errorf("runtime directory does not match configured root and exact binding")
	}
	client := &Client{RuntimeDirectory: directory, MaxSnapshotAge: time.Minute}
	return (&exactStartController{launcher: c.launcher, controller: client, policy: c.policy}).startExact(ctx, request)
}

func (c *ExactController) Snapshot(ctx context.Context, binding Binding) (Snapshot, error) {
	directory, err := c.runtimeDirectory(binding)
	if err != nil {
		return Snapshot{}, err
	}
	return (&Client{RuntimeDirectory: directory, MaxSnapshotAge: time.Minute}).Snapshot(ctx, binding)
}

func (c *ExactController) Stop(ctx context.Context, binding Binding) error {
	directory, err := c.runtimeDirectory(binding)
	if err != nil {
		return err
	}
	return (&Client{RuntimeDirectory: directory, MaxSnapshotAge: time.Minute}).Stop(ctx, binding)
}

func (c *ExactController) runtimeDirectory(binding Binding) (string, error) {
	if c == nil || !canonicalAbsolute(c.runtimeRoot) || c.launcher == nil || !binding.valid() {
		return "", fmt.Errorf("root supervisor dependencies and binding are required")
	}
	directory := filepath.Join(c.runtimeRoot, binding.Domain, binding.SessionID, binding.Generation)
	if !canonicalAbsolute(directory) || filepath.Dir(filepath.Dir(filepath.Dir(directory))) != c.runtimeRoot {
		return "", fmt.Errorf("invalid exact runtime directory")
	}
	return directory, nil
}

func (c *exactStartController) startExact(ctx context.Context, request LaunchRequest) (Snapshot, error) {
	if c == nil || c.launcher == nil || c.controller == nil {
		return Snapshot{}, fmt.Errorf("exact supervisor dependencies are required")
	}
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	policy := c.policy
	if policy.timeout <= 0 || policy.interval <= 0 {
		policy = productionStartupPolicy
	}
	ctx, cancel := context.WithTimeout(ctx, policy.timeout)
	defer cancel()
	for {
		state, err := classifyExactGeneration(request)
		if err != nil {
			return Snapshot{}, err
		}
		if state == exactGenerationLive {
			got, err := awaitLiveSnapshot(ctx, request, policy, c.controller.Snapshot)
			if errors.Is(err, errExactGenerationTransition) {
				continue
			}
			if err != nil {
				return got, err
			}
			return c.bootstrapExact(ctx, request.Binding)
		}
		if err := c.launcher.Launch(ctx, request); err == nil {
			got, err := awaitLiveSnapshot(ctx, request, policy, c.controller.Snapshot)
			if errors.Is(err, errExactGenerationTransition) {
				continue
			}
			if err != nil {
				return got, err
			}
			return c.bootstrapExact(ctx, request.Binding)
		} else if errors.Is(err, errExactGenerationTransition) {
			continue
		} else if !errors.Is(err, errGenerationAlreadyOwned) {
			return Snapshot{}, err
		}
		if err := ctx.Err(); err != nil {
			return Snapshot{}, err
		}
		timer := time.NewTimer(policy.interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return Snapshot{}, ctx.Err()
		case <-timer.C:
		}
	}
}

func (c *exactStartController) bootstrapExact(ctx context.Context, binding Binding) (Snapshot, error) {
	snapshot, err := c.controller.Bootstrap(ctx, binding)
	if err != nil {
		return snapshot, err
	}
	if snapshot.Binding != binding || !snapshotBootstrapped(snapshot) {
		return Snapshot{}, fmt.Errorf("supervisor did not provide an exact bootstrapped snapshot")
	}
	return snapshot, nil
}

// awaitLiveSnapshot watches only an already-owned exact generation. A valid
// transition out of live ownership is a private instruction to re-enter exact
// admission; malformed or ambiguous state remains an ordinary fail-closed
// error and cannot become retry authority.
func awaitLiveSnapshot(ctx context.Context, request LaunchRequest, policy startupPolicy, snapshot func(context.Context, Binding) (Snapshot, error)) (Snapshot, error) {
	checkLive := func() error {
		state, err := classifyExactGeneration(request)
		if err != nil {
			return err
		}
		if state != exactGenerationLive {
			return errExactGenerationTransition
		}
		return nil
	}
	return awaitSnapshotChecked(ctx, request.Binding, policy, checkLive, snapshot)
}

func awaitSnapshot(ctx context.Context, binding Binding, policy startupPolicy, snapshot func(context.Context, Binding) (Snapshot, error)) (Snapshot, error) {
	return awaitSnapshotChecked(ctx, binding, policy, nil, snapshot)
}

func awaitSnapshotChecked(ctx context.Context, binding Binding, policy startupPolicy, check func() error, snapshot func(context.Context, Binding) (Snapshot, error)) (Snapshot, error) {
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
		if check != nil {
			if err := check(); err != nil {
				return Snapshot{}, err
			}
		}
		got, err := snapshot(ctx, binding)
		if err == nil {
			if got.Binding != binding {
				return Snapshot{}, fmt.Errorf("supervisor binding mismatch")
			}
			if snapshotStarted(got) {
				return got, nil
			}
			last = fmt.Errorf("supervisor snapshot is not started: %s", boundedDiagnostic(got.Diagnostic))
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
