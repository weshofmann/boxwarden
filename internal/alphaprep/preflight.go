// Package alphaprep composes admitted host and domain prerequisites for the
// automatic alpha base-preparation path. The preflight has no mutation power.
package alphaprep

import (
	"context"
	"errors"
	"fmt"

	"github.com/weshofmann/boxwarden/internal/config"
	"github.com/weshofmann/boxwarden/internal/hostx"
	"github.com/weshofmann/boxwarden/internal/sshx"
)

type HostRuntimeChecker interface {
	CheckRuntime(context.Context, hostx.Request) (hostx.RuntimeExpectation, error)
}

type DomainCAChecker interface {
	Check(context.Context, sshx.Domain, []sshx.Domain) (sshx.CAIdentity, error)
}

// Preflight checks the full admitted host and CA scope before a candidate or
// attempt is created. A selected domain supplied by a caller must match the
// loaded configuration exactly; no cross-domain fallback is possible.
func Preflight(ctx context.Context, loaded config.Config, selected config.Domain, doctor HostRuntimeChecker, ca DomainCAChecker) (hostx.RuntimeExpectation, error) {
	if doctor == nil || ca == nil {
		return hostx.RuntimeExpectation{}, errors.New("host doctor and management CA checker are required")
	}
	admitted, err := loaded.Domain(string(selected.ID))
	if err != nil || admitted != selected {
		return hostx.RuntimeExpectation{}, errors.New("selected domain differs from admitted configuration")
	}
	host, err := loaded.HostAdmission()
	if err != nil {
		return hostx.RuntimeExpectation{}, err
	}
	runtime, err := doctor.CheckRuntime(ctx, hostx.Request{
		ConfiguredStateRoots: host.ConfiguredStateRoots,
		TartPath:             host.Host.TartExecutable,
		TartHome:             host.Host.TartHome,
		SoftnetPath:          host.Host.SoftnetSource,
	})
	if err != nil {
		return hostx.RuntimeExpectation{}, fmt.Errorf("host runtime preflight: %w", err)
	}
	configured := loaded.Domains()
	domains := make([]sshx.Domain, 0, len(configured))
	for _, item := range configured {
		domains = append(domains, sshx.Domain{ID: item.ID, StateRoot: item.StateRoot})
	}
	identity, err := ca.Check(ctx, sshx.Domain{ID: selected.ID, StateRoot: selected.StateRoot}, domains)
	if err != nil {
		return hostx.RuntimeExpectation{}, fmt.Errorf("management CA preflight: %w", err)
	}
	if identity.Version != 1 || identity.Domain != selected.ID || identity.StateRoot != selected.StateRoot {
		return hostx.RuntimeExpectation{}, errors.New("management CA preflight returned a different domain identity")
	}
	return runtime, nil
}
