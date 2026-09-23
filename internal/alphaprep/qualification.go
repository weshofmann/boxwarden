package alphaprep

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/weshofmann/boxwarden/internal/alphaqual"
	"github.com/weshofmann/boxwarden/internal/backend"
	"github.com/weshofmann/boxwarden/internal/config"
	"github.com/weshofmann/boxwarden/internal/golden"
	"github.com/weshofmann/boxwarden/internal/privateacl"
	"github.com/weshofmann/boxwarden/internal/session"
	"github.com/weshofmann/boxwarden/internal/sessionruntime"
	"github.com/weshofmann/boxwarden/internal/supervisor"
)

type qualificationBackend interface {
	backend.Observer
	backend.Creator
}

type revisionRegistrar struct {
	domain   config.Domain
	observer backend.Observer
}

func (r revisionRegistrar) RegisterRevision(ctx context.Context, candidate string) (golden.Record, error) {
	return golden.RegisterRevision(ctx, r.domain, candidate, r.observer)
}

type qualificationLifecycle struct {
	creator *session.Service
	runner  *session.Service
}

func (l qualificationLifecycle) CreateFreshFromRevision(ctx context.Context, name string, mode session.Mode, revision string) (session.FreshCreation, error) {
	return l.creator.CreateFreshFromRevision(ctx, name, mode, revision)
}

func (l qualificationLifecycle) Start(ctx context.Context, name string) (session.Record, error) {
	return l.runner.Start(ctx, name)
}

func (l qualificationLifecycle) Stop(ctx context.Context, name string) (session.Record, error) {
	return l.runner.Stop(ctx, name)
}

// NewProductionQualifier uses the public create-only, start, and stop paths on
// one selected domain. The caller supplies a trusted host guest-inspection
// implementation. A passing prepared-base cache receipt remains narrower
// than final graphical, workspace, and export acceptance.
func NewProductionQualifier(loaded config.Config, selected config.Domain, configPath string, vm qualificationBackend, inspector alphaqual.GuestInspector) (*alphaqual.Qualifier, error) {
	admitted, err := loaded.Domain(string(selected.ID))
	if err != nil || admitted != selected {
		return nil, errors.New("qualification domain differs from admitted configuration")
	}
	if vm == nil || inspector == nil {
		return nil, errors.New("qualification backend and guest inspector are required")
	}
	runner, err := sessionruntime.NewStarter(loaded, selected, configPath)
	if err != nil {
		return nil, fmt.Errorf("qualification public lifecycle: %w", err)
	}
	snapshots, err := supervisor.NewExactSnapshotReader(filepath.Join(selected.StateRoot, "runtime"))
	if err != nil {
		return nil, err
	}
	return alphaqual.New(alphaqual.Dependencies{
		Domain:    selected.ID,
		Registrar: revisionRegistrar{domain: selected, observer: vm},
		Lifecycle: qualificationLifecycle{creator: session.NewService(selected, vm, vm), runner: runner},
		Observer:  vm, Snapshots: snapshots, Inspector: inspector,
		ACL: privateacl.OSInspector{},
	})
}

var _ alphaqual.RevisionRegistrar = revisionRegistrar{}
var _ alphaqual.Lifecycle = qualificationLifecycle{}
