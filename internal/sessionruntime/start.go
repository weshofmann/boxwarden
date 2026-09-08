package sessionruntime

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/weshofmann/boxwarden/internal/backend/tart"
	"github.com/weshofmann/boxwarden/internal/config"
	"github.com/weshofmann/boxwarden/internal/execx"
	"github.com/weshofmann/boxwarden/internal/hostx"
	"github.com/weshofmann/boxwarden/internal/session"
	"github.com/weshofmann/boxwarden/internal/sshx"
	"github.com/weshofmann/boxwarden/internal/supervisor"
)

// NewStarter constructs the parent start service after configuration/domain
// admission. Construction creates no CA, generation, serial state, or child.
func NewStarter(loaded config.Config, selected config.Domain, path string) (*session.Service, error) {
	deps, err := startDependencies(loaded, selected, path)
	if err != nil {
		return nil, err
	}
	return session.NewStartService(selected, deps), nil
}

func startDependencies(loaded config.Config, selected config.Domain, path string) (session.StartDependencies, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return session.StartDependencies{}, fmt.Errorf("configuration path must be canonical and absolute")
	}
	admitted, err := loaded.Domain(string(selected.ID))
	if err != nil || admitted != selected {
		return session.StartDependencies{}, fmt.Errorf("selected domain does not match admitted configuration")
	}
	host, err := loaded.HostAdmission()
	if err != nil {
		return session.StartDependencies{}, err
	}
	launcher, err := supervisor.NewDetachedLauncher()
	if err != nil {
		return session.StartDependencies{}, err
	}
	runtimeRoot := filepath.Join(selected.StateRoot, "runtime")
	controller, err := supervisor.NewExactController(runtimeRoot, launcher)
	if err != nil {
		return session.StartDependencies{}, err
	}
	var domains []sshx.Domain
	for _, d := range loaded.Domains() {
		domains = append(domains, sshx.Domain{ID: d.ID, StateRoot: d.StateRoot})
	}
	ca := sshx.NewCAStore(sshx.CAStoreOptions{Runner: sshx.NewExecRunner(), Identity: sshx.OSIdentity{}})
	return session.StartDependencies{
		Observer:    tart.NewQualifiedObserver(execx.OSRunner{MaxOutputBytes: 1 << 20}, host.Host.TartExecutable, host.Host.TartHome),
		Host:        hostx.NewSystemDoctor(),
		HostRequest: hostx.Request{ConfiguredStateRoots: host.ConfiguredStateRoots, TartPath: host.Host.TartExecutable, TartHome: host.Host.TartHome, SoftnetPath: host.Host.SoftnetSource},
		CA:          caCheckOnly{check: ca.Check}, ConfiguredDomains: domains,
		Supervisor: controller, RuntimeRoot: runtimeRoot, ConfigPath: path,
		NewGeneration: sshx.RandomUUID, Now: time.Now,
	}, nil
}

// caCheckOnly deliberately removes CA creation from start's capability surface.
type caCheckOnly struct {
	check func(context.Context, sshx.Domain, []sshx.Domain) (sshx.CAIdentity, error)
}

func (c caCheckOnly) Check(ctx context.Context, selected sshx.Domain, all []sshx.Domain) (sshx.CAIdentity, error) {
	return c.check(ctx, selected, all)
}

// RunRequest is the production internal command's authoritative child entry.
func RunRequest(ctx context.Context, path string) error {
	return supervisor.Run(ctx, path, NewOwner())
}
