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
	rebuilder, starter, err := admittedRebuilder(ctx, loaded, selected, configPath)
	if err != nil {
		return session.Record{}, err
	}
	if intentDigest != "" {
		return rebuilder.ExecuteWithIntent(ctx, name, revision, intentDigest, starter)
	}
	return rebuilder.Execute(ctx, name, revision, starter)
}

// CompleteRebuild executes only the exact candidate retained by a project.
func CompleteRebuild(ctx context.Context, loaded config.Config, selected config.Domain, configPath string, expected session.RebuildJournal) (session.Record, error) {
	rebuilder, starter, err := admittedRebuilder(ctx, loaded, selected, configPath)
	if err != nil {
		return session.Record{}, err
	}
	return rebuilder.ExecutePrepared(ctx, expected.SessionName, expected, starter)
}

// PrepareRebuild explicitly reserves a fresh stopped candidate even when the
// selected base equals the current system's base. The durable session journal
// makes subsequent Rebuild calls exact retries rather than another reset.
func PrepareRebuild(ctx context.Context, loaded config.Config, selected config.Domain, configPath, name, revision string) (session.RebuildJournal, error) {
	rebuilder, _, err := admittedRebuilder(ctx, loaded, selected, configPath)
	if err != nil {
		return session.RebuildJournal{}, err
	}
	return rebuilder.PrepareCandidate(ctx, name, revision)
}

func admittedRebuilder(ctx context.Context, loaded config.Config, selected config.Domain, configPath string) (*session.RebuildService, *session.Service, error) {
	admittedDomain, err := loaded.Domain(string(selected.ID))
	if err != nil || admittedDomain != selected {
		return nil, nil, fmt.Errorf("rebuild requires exact configured domain")
	}
	host, err := loaded.HostAdmission()
	if err != nil {
		return nil, nil, err
	}
	request := hostx.Request{ConfiguredStateRoots: host.ConfiguredStateRoots, TartPath: host.Host.TartExecutable,
		TartHome: host.Host.TartHome, SoftnetPath: host.Host.SoftnetSource}
	if _, err := hostx.NewSystemDoctor().CheckRuntime(ctx, request); err != nil {
		return nil, nil, fmt.Errorf("admit host toolchain before rebuild: %w", err)
	}
	starter, err := NewStarter(loaded, selected, configPath)
	if err != nil {
		return nil, nil, err
	}
	runner := execx.OSRunner{MaxOutputBytes: 1 << 20}
	observer := tart.NewQualifiedObserver(runner, host.Host.TartExecutable, host.Host.TartHome)
	rebuilder := session.NewRebuildService(selected, session.RebuildDependencies{
		Observer: observer, Creator: observer,
		Deleter: tart.NewDeleter(runner, host.Host.TartExecutable, host.Host.TartHome),
		Gate:    workspacex.WithStoppedRebuildGate,
		Pins:    sshx.NewPinStore(sshx.Domain{ID: selected.ID, StateRoot: selected.StateRoot}),
	})
	return rebuilder, starter, nil
}
