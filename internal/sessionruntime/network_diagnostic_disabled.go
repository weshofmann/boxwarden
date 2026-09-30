//go:build !n1diagnostic || n1candidate

package sessionruntime

import (
	"context"
	"errors"
	"github.com/weshofmann/boxwarden/internal/backend"
	"github.com/weshofmann/boxwarden/internal/backend/tart"
	"github.com/weshofmann/boxwarden/internal/config"
	"github.com/weshofmann/boxwarden/internal/hostx"
	"github.com/weshofmann/boxwarden/internal/session"
	"github.com/weshofmann/boxwarden/internal/supervisor"
)

type ownerDiagnosticDependencies struct{}
type ownerDiagnosticState struct{}

func initializeOwnerDiagnostic(*Owner) {}
func configureDiagnosticStart(*session.StartDependencies, config.Config, config.Domain, string) error {
	return nil
}
func (*Owner) acquireDiagnosticGuard(context.Context, config.Config, config.Domain, supervisor.LaunchRequest, hostx.RuntimeExpectation) error {
	return nil
}
func (*Owner) finishDiagnosticPrechild(err error) error      { return err }
func (*Owner) classifyDiagnosticCleanup(err error) error     { return err }
func (*Owner) classifyDiagnosticLaunchError(err error) error { return err }
func (*Owner) finishDiagnosticWait(err error) error          { return err }
func (*Owner) startDiagnosticOrOrdinary(ctx context.Context, l backend.Starter, _ tart.LaunchConfig, r backend.StartRequest, _ supervisor.Binding) (backend.Handle, error) {
	return l.Start(ctx, r)
}

// Preserve published ordinary Start propagation of both error causes.
func (*Owner) finishUnclaimedDiagnosticDisks(result, closeErr error) error {
	return errors.Join(result, closeErr)
}
