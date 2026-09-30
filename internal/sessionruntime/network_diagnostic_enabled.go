//go:build n1diagnostic && !n1candidate

package sessionruntime

import (
	"context"
	"errors"
	"fmt"
	"github.com/weshofmann/boxwarden/internal/backend"
	"github.com/weshofmann/boxwarden/internal/backend/tart"
	"github.com/weshofmann/boxwarden/internal/config"
	"github.com/weshofmann/boxwarden/internal/hostx"
	"github.com/weshofmann/boxwarden/internal/networkdiag"
	"github.com/weshofmann/boxwarden/internal/session"
	"github.com/weshofmann/boxwarden/internal/supervisor"
	"sync"
)

type ownerDiagnosticDependencies struct {
	acquire func(context.Context, config.Config, config.Domain, supervisor.LaunchRequest, hostx.RuntimeExpectation) (session.LaunchGuard, error)
	launch  func(context.Context, tart.LaunchConfig, backend.StartRequest, supervisor.Binding) (backend.Handle, error)
	pair    func(context.Context, networkdiag.Arm) ([2]networkdiag.Inspection, error)
}
type ownerDiagnosticState struct {
	guard     session.LaunchGuard
	uncertain bool
	mu        sync.Mutex
	attempted bool
	arm       networkdiag.Arm
	armed     networkdiag.Armed
	before    [2]networkdiag.Inspection
}
type enrollmentLaunchGuard struct {
	session.LaunchGuard
	path string
	c    config.Config
	d    config.Domain
}

func (g *enrollmentLaunchGuard) Revalidate(ctx context.Context) error {
	current, err := config.LoadN1Enrollment(g.path)
	if err != nil || !sameN1Configuration(current, g.c, g.d) {
		return config.ErrN1Enrollment
	}
	return g.LaunchGuard.Revalidate(ctx)
}
func initializeOwnerDiagnostic(o *Owner) {
	o.deps.diagnostic.acquire = func(ctx context.Context, c config.Config, d config.Domain, r supervisor.LaunchRequest, e hostx.RuntimeExpectation) (session.LaunchGuard, error) {
		current, err := config.LoadN1Enrollment(r.HostConfigPath)
		if err != nil {
			return nil, err
		}
		if !sameN1Configuration(current, c, d) || r.Binding.Domain != config.N1Domain || r.SessionRecordName != config.N1CandidateName {
			return nil, config.ErrN1Enrollment
		}
		admitted, err := current.HostAdmission()
		if err != nil {
			return nil, err
		}
		g, err := hostx.NewSystemDoctor().AcquireDiagnosticLaunch(ctx, hostx.Request{ConfiguredStateRoots: admitted.ConfiguredStateRoots, TartPath: admitted.Host.TartExecutable, TartHome: admitted.Host.TartHome, SoftnetPath: admitted.Host.SoftnetSource}, e)
		if g == nil {
			return nil, err
		}
		return &enrollmentLaunchGuard{g, r.HostConfigPath, c, d}, err
	}
	o.deps.diagnostic.launch = func(ctx context.Context, c tart.LaunchConfig, r backend.StartRequest, b supervisor.Binding) (backend.Handle, error) {
		nonce, err := o.deps.newNonce()
		if err != nil {
			return nil, err
		}
		observer := o.deps.observer(c.TartPath, c.TartHome)
		typed, ok := observer.(interface {
			ObserveDiagnosticMAC(context.Context, string) (tart.DiagnosticMACObservation, error)
		})
		if !ok {
			return nil, fmt.Errorf("diagnostic MAC observer unavailable")
		}
		mac, err := typed.ObserveDiagnosticMAC(ctx, b.BackendObject)
		if err != nil {
			return nil, err
		}
		if err := o.diagnostic.guard.Revalidate(ctx); err != nil {
			return nil, err
		}
		return tart.NewDiagnosticLauncher(c, tart.DiagnosticLaunchBinding{Generation: b.Generation, Nonce: nonce, CandidateMAC: mac.MAC}).Start(ctx, r)
	}
}
func sameN1Configuration(a, b config.Config, d config.Domain) bool {
	ah, ae := a.HostAdmission()
	bh, be := b.HostAdmission()
	ad, err := a.Domain(config.N1Domain)
	return ae == nil && be == nil && err == nil && len(a.Domains()) == 1 && len(b.Domains()) == 1 && ah.Host == bh.Host && ad.ID == d.ID && ad.StateRoot == d.StateRoot && ad.WorkspaceStorage != nil && d.WorkspaceStorage != nil && *ad.WorkspaceStorage == *d.WorkspaceStorage
}
func configureDiagnosticStart(deps *session.StartDependencies, c config.Config, d config.Domain, path string) error {
	current, err := config.LoadN1Enrollment(path)
	if err != nil || !sameN1Configuration(current, c, d) {
		return config.ErrN1Enrollment
	}
	deps.AdmitLaunchRecord = func(r session.Record) error {
		if string(r.Domain) != config.N1Domain || string(r.Name) != config.N1CandidateName {
			return config.ErrN1Enrollment
		}
		return nil
	}
	deps.AcquireLaunchGuard = func(ctx context.Context, e session.RuntimeAdmission) (session.LaunchGuard, error) {
		current, err := config.LoadN1Enrollment(path)
		if err != nil || !sameN1Configuration(current, c, d) {
			return nil, config.ErrN1Enrollment
		}
		g, err := hostx.NewSystemDoctor().AcquireDiagnosticLaunch(ctx, deps.HostRequest, e)
		if g == nil {
			return nil, err
		}
		return &enrollmentLaunchGuard{g, path, c, d}, err
	}
	return nil
}
func (o *Owner) acquireDiagnosticGuard(ctx context.Context, c config.Config, d config.Domain, r supervisor.LaunchRequest, e hostx.RuntimeExpectation) error {
	if o.deps.diagnostic.acquire == nil {
		return nil
	} // Only private injected test compositions omit production wiring.
	g, err := o.deps.diagnostic.acquire(ctx, c, d, r, e)
	if err != nil {
		if g != nil {
			err = errors.Join(err, g.Release())
		}
		return err
	}
	if g == nil {
		return fmt.Errorf("diagnostic guard unavailable")
	}
	if err = g.Revalidate(ctx); err != nil {
		return errors.Join(err, g.Release())
	}
	o.diagnostic.guard = g
	return nil
}
func (o *Owner) finishDiagnosticPrechild(err error) error {
	if o.handle != nil || o.diagnostic.guard == nil {
		return err
	}
	if errors.Is(err, supervisor.ErrRuntimeCleanupUnproven) {
		return err
	}
	return errors.Join(err, o.classifyDiagnosticCleanup(o.diagnostic.guard.Release()))
}
func (o *Owner) classifyDiagnosticCleanup(err error) error {
	if err == nil {
		return nil
	}
	o.diagnostic.uncertain = true
	return fmt.Errorf("%w: %w", supervisor.ErrRuntimeCleanupUnproven, err)
}
func (o *Owner) classifyDiagnosticLaunchError(err error) error {
	if errors.Is(err, tart.ErrScratchCleanupUnproven) || errors.Is(err, tart.ErrReapUnproven) || errors.Is(err, tart.ErrDiagnosticCleanupUnproven) {
		return o.classifyDiagnosticCleanup(err)
	}
	return err
}
func (o *Owner) finishDiagnosticWait(err error) error {
	if o.diagnostic.uncertain {
		return errors.Join(err, supervisor.ErrRuntimeCleanupUnproven)
	}
	if o.diagnostic.guard == nil || errors.Is(err, supervisor.ErrRuntimeCleanupUnproven) {
		return err
	}
	return errors.Join(err, o.classifyDiagnosticCleanup(o.diagnostic.guard.Release()))
}
func (o *Owner) startDiagnosticOrOrdinary(ctx context.Context, l backend.Starter, c tart.LaunchConfig, r backend.StartRequest, b supervisor.Binding) (backend.Handle, error) {
	if o.deps.diagnostic.launch == nil {
		return l.Start(ctx, r)
	}
	if o.diagnostic.guard == nil {
		return nil, fmt.Errorf("diagnostic Owner guard unavailable")
	}
	return o.deps.diagnostic.launch(ctx, c, r, b)
}

func (o *Owner) finishUnclaimedDiagnosticDisks(result, closeErr error) error {
	return errors.Join(result, o.classifyDiagnosticCleanup(closeErr))
}
