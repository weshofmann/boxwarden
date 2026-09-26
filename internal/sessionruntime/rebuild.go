package sessionruntime

import (
	"context"
	"fmt"

	"github.com/weshofmann/boxwarden/internal/backend/tart"
	"github.com/weshofmann/boxwarden/internal/config"
	"github.com/weshofmann/boxwarden/internal/execx"
	"github.com/weshofmann/boxwarden/internal/hostx"
	"github.com/weshofmann/boxwarden/internal/session"
	"github.com/weshofmann/boxwarden/internal/sshx"
	"github.com/weshofmann/boxwarden/internal/workspacex"
)

// Rebuild composes the public alpha's exact domain-scoped Tart mechanics with
// the common durable rebuild driver. The host toolchain is re-admitted before
// clone or deletion; the detached child independently re-admits it at launch.
func Rebuild(ctx context.Context, loaded config.Config, selected config.Domain, configPath, name, revision, intentDigest string) (session.Record, error) {
	admittedDomain, err := loaded.Domain(string(selected.ID))
	if err != nil || admittedDomain != selected {
		return session.Record{}, fmt.Errorf("rebuild requires exact configured domain")
	}
	host, err := loaded.HostAdmission()
	if err != nil {
		return session.Record{}, err
	}
	request := hostx.Request{ConfiguredStateRoots: host.ConfiguredStateRoots, TartPath: host.Host.TartExecutable,
		TartHome: host.Host.TartHome, SoftnetPath: host.Host.SoftnetSource}
	if _, err := hostx.NewSystemDoctor().CheckRuntime(ctx, request); err != nil {
		return session.Record{}, fmt.Errorf("admit host toolchain before rebuild: %w", err)
	}
	starter, err := NewStarter(loaded, selected, configPath)
	if err != nil {
		return session.Record{}, err
	}
	runner := execx.OSRunner{MaxOutputBytes: 1 << 20}
	observer := tart.NewQualifiedObserver(runner, host.Host.TartExecutable, host.Host.TartHome)
	rebuilder := session.NewRebuildService(selected, session.RebuildDependencies{
		Observer: observer, Creator: observer,
		Deleter: tart.NewDeleter(runner, host.Host.TartExecutable, host.Host.TartHome),
		Gate:    workspacex.WithStoppedRebuildGate,
		Pins:    sshx.NewPinStore(sshx.Domain{ID: selected.ID, StateRoot: selected.StateRoot}),
	})
	if intentDigest != "" {
		return rebuilder.ExecuteWithIntent(ctx, name, revision, intentDigest, starter)
	}
	return rebuilder.Execute(ctx, name, revision, starter)
}
