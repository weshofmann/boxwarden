package session

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/weshofmann/boxwarden/internal/backend"
	"github.com/weshofmann/boxwarden/internal/config"
	"github.com/weshofmann/boxwarden/internal/domain"
	"github.com/weshofmann/boxwarden/internal/hostx"
	"github.com/weshofmann/boxwarden/internal/lock"
	"github.com/weshofmann/boxwarden/internal/sshx"
	"github.com/weshofmann/boxwarden/internal/supervisor"
)

const maxReadySnapshotAge = 90 * time.Second

// RuntimeAdmission contains inspected public host prerequisites.
// The later child composition must independently reload and verify them.
type RuntimeAdmission = hostx.RuntimeExpectation

type RuntimeChecker interface {
	CheckRuntime(context.Context, hostx.Request) (RuntimeAdmission, error)
}

// CAValidator deliberately receives the complete configured-domain collection
// so the CA store retains duplicate and partial-state rejection.
type CAValidator interface {
	Check(context.Context, sshx.Domain, []sshx.Domain) (sshx.CAIdentity, error)
}

// SupervisorControl is the complete session-facing authority. It cannot mint
// paths, process handles, or generic supervisor internals.
type SupervisorControl interface {
	StartExact(context.Context, supervisor.LaunchRequest) (supervisor.Snapshot, error)
	Snapshot(context.Context, supervisor.Binding) (supervisor.Snapshot, error)
	Ready(context.Context, supervisor.Binding) (supervisor.Snapshot, error)
	Stop(context.Context, supervisor.Binding) error
	Quiesced(context.Context, supervisor.Binding) (bool, error)
}

// WorkspaceLifecycle owns the volume-first batch transactions. Session code
// cannot import the volume package because volume records bind sessions.
type WorkspaceLifecycle interface {
	PrepareStart(context.Context, string, domain.ID, Record, string, backend.Observer) (Record, error)
	VerifyUses(context.Context, string, domain.ID, Record) error
	ReleaseUses(context.Context, string, domain.ID, Record, backend.Observer) error
}

type StartDependencies struct {
	Observer          backend.Observer
	Host              RuntimeChecker
	HostRequest       hostx.Request
	CA                CAValidator
	ConfiguredDomains []sshx.Domain
	Supervisor        SupervisorControl
	Workspaces        WorkspaceLifecycle
	RuntimeRoot       string
	ConfigPath        string
	NewGeneration     func() (string, error)
	Now               func() time.Time
}

// NewStartService composes the narrow dependencies needed for start without
// widening the existing create service's backend mutation surface.
func NewStartService(configured config.Domain, dependencies StartDependencies) *Service {
	return &Service{domain: configured, observer: dependencies.Observer, start: &dependencies}
}

func (s *Service) Start(ctx context.Context, rawName string) (record Record, err error) {
	if s == nil || s.start == nil {
		return Record{}, fmt.Errorf("session start dependencies are required")
	}
	if err := s.validStartDependencies(); err != nil {
		return Record{}, err
	}
	domainID, _ := domain.Parse(string(s.domain.ID))
	name, err := ParseName(rawName)
	if err != nil {
		return Record{}, err
	}
	transition, err := lock.Acquire(ctx, s.domain.StateRoot, "transition-"+string(domainID)+"-"+string(name))
	if err != nil {
		return Record{}, fmt.Errorf("acquire session transition lock: %w", err)
	}
	defer func() {
		if releaseErr := transition.Release(); err == nil && releaseErr != nil {
			err = fmt.Errorf("release session transition lock: %w", releaseErr)
		}
	}()
	held, err := lock.AcquireSession(ctx, s.domain.StateRoot, string(domainID), string(name))
	if err != nil {
		return Record{}, fmt.Errorf("acquire session lock: %w", err)
	}
	defer func() {
		if releaseErr := held.Release(); err == nil && releaseErr != nil {
			err = fmt.Errorf("release session lock: %w", releaseErr)
		}
	}()
	if err := RequireNoRebuild(s.domain.StateRoot, domainID, string(name)); err != nil {
		return Record{}, err
	}

	record, err = LoadRecord(s.domain.StateRoot, string(domainID), string(name))
	if err != nil {
		return Record{}, fmt.Errorf("load session record: %w", err)
	}
	_, _, err = s.admitStartPrerequisites(ctx)
	if err != nil {
		return Record{}, err
	}
	if record.Backend.Kind != "tart" || record.Backend.ObjectID == "" {
		return Record{}, fmt.Errorf("session backend binding is unsupported")
	}
	if record.IntendedState == StateStarting || record.IntendedState == StateRunning {
		if err := s.start.Workspaces.VerifyUses(ctx, s.domain.StateRoot, domainID, record); err != nil {
			return Record{}, fmt.Errorf("verify exact workspace uses: %w", err)
		}
	}

	switch record.IntendedState {
	case StateStopped:
		observation, observeErr := s.observeExact(ctx, record.Backend.ObjectID)
		if observeErr != nil {
			return Record{}, fmt.Errorf("observe stopped session backend: %w", observeErr)
		}
		if !observation.Exists || observation.State != backend.ObjectStopped {
			return Record{}, fmt.Errorf("stopped session does not match one stopped backend object")
		}
		if record.Version == recordVersionV1 {
			legacy := record
			if saveErr := SaveRecord(s.domain.StateRoot, domainID, record); saveErr != nil {
				return Record{}, fmt.Errorf("upgrade stopped session before workspace reservation: %w", saveErr)
			}
			record, err = LoadRecord(s.domain.StateRoot, string(domainID), string(name))
			if err != nil || record.Version != recordVersion || record.IntendedState != StateStopped || !sameStoppedIdentity(legacy, record) {
				return Record{}, fmt.Errorf("stopped session upgrade did not retain exact identity: %w", err)
			}
		}
		if releaseErr := held.Release(); releaseErr != nil {
			return Record{}, fmt.Errorf("release session lock before stopped workspace reconciliation: %w", releaseErr)
		}
		if releaseErr := s.start.Workspaces.ReleaseUses(ctx, s.domain.StateRoot, domainID, record, s.observer); releaseErr != nil {
			return Record{}, fmt.Errorf("reconcile stopped workspace uses before start: %w", releaseErr)
		}
		held, err = lock.AcquireSession(ctx, s.domain.StateRoot, string(domainID), string(name))
		if err != nil {
			return Record{}, fmt.Errorf("reacquire session lock after stopped workspace reconciliation: %w", err)
		}
		current, loadErr := LoadRecord(s.domain.StateRoot, string(domainID), string(name))
		if loadErr != nil || current != record {
			return Record{}, fmt.Errorf("stopped session changed during workspace reconciliation: %w", loadErr)
		}
		generation, generationErr := s.start.NewGeneration()
		if generationErr != nil || !validUUID(generation) {
			return Record{}, fmt.Errorf("generate start generation: %w", generationErr)
		}
		if releaseErr := held.Release(); releaseErr != nil {
			return Record{}, fmt.Errorf("release session lock before workspace reservation: %w", releaseErr)
		}
		started, prepareErr := s.start.Workspaces.PrepareStart(ctx, s.domain.StateRoot, domainID, record, generation, s.observer)
		if prepareErr != nil {
			return Record{}, fmt.Errorf("reserve workspaces and persist starting session: %w", prepareErr)
		}
		held, err = lock.AcquireSession(ctx, s.domain.StateRoot, string(domainID), string(name))
		if err != nil {
			return Record{}, fmt.Errorf("reacquire session lock after workspace reservation: %w", err)
		}
		record, err = LoadRecord(s.domain.StateRoot, string(domainID), string(name))
		if err != nil || record != started || record.IntendedState != StateStarting {
			return Record{}, fmt.Errorf("starting session changed after workspace reservation: %w", err)
		}
	case StateStarting:
		observation, observeErr := s.observeExact(ctx, record.Backend.ObjectID)
		if observeErr != nil {
			return Record{}, fmt.Errorf("observe starting session backend: %w", observeErr)
		}
		if !observation.Exists {
			return Record{}, fmt.Errorf("starting session backend is missing")
		}
		switch observation.State {
		case backend.ObjectRunning, backend.ObjectStopped:
			snapshot, snapshotErr := s.start.Supervisor.Snapshot(ctx, startBinding(record))
			if snapshotErr != nil {
				if observation.State == backend.ObjectRunning {
					return Record{}, fmt.Errorf("inspect exact starting generation: %w", snapshotErr)
				}
				// No exact owner is available. StartExact must independently
				// classify the generation before any launch or bootstrap.
				break
			}
			now := s.start.Now()
			if snapshot.Binding != startBinding(record) || snapshot.ObservedAt.IsZero() || snapshot.ObservedAt.After(now) || now.Sub(snapshot.ObservedAt) > maxReadySnapshotAge {
				return Record{}, fmt.Errorf("supervisor did not provide a fresh exact running snapshot")
			}
			if !snapshot.BackendRunning {
				if observation.State == backend.ObjectRunning {
					return Record{}, fmt.Errorf("supervisor did not provide a fresh exact running snapshot")
				}
				break
			}
			if snapshot.SerialHealthy && snapshot.PinPresent {
				if snapshot.CertificateCurrent && snapshot.ProbeOK && snapshot.ZoneMatches {
					return s.persistReady(record, snapshot)
				}
				ready, readyErr := s.start.Supervisor.Ready(ctx, startBinding(record))
				if readyErr != nil {
					return Record{}, fmt.Errorf("converge exact starting generation: %w", readyErr)
				}
				return s.acceptStarted(record, ready)
			}
			if !snapshot.SerialHealthy {
				if releaseErr := held.Release(); releaseErr != nil {
					return Record{}, fmt.Errorf("release session lock before poisoned-generation stop: %w", releaseErr)
				}
				if stopErr := s.start.Supervisor.Stop(ctx, startBinding(record)); stopErr != nil {
					return Record{}, fmt.Errorf("stop poisoned exact serial generation: %w", stopErr)
				}
				held, err = lock.AcquireSession(ctx, s.domain.StateRoot, string(domainID), string(name))
				if err != nil {
					return Record{}, fmt.Errorf("reacquire session lock after poisoned-generation stop: %w", err)
				}
				current, loadErr := LoadRecord(s.domain.StateRoot, string(domainID), string(name))
				if loadErr != nil || !sameLaunchIdentity(record, current) || current.IntendedState != StateStarting {
					return Record{}, fmt.Errorf("poisoned-generation stop changed starting record: %w", loadErr)
				}
				record = current
				stopped, stoppedErr := s.observeExact(ctx, record.Backend.ObjectID)
				if stoppedErr != nil {
					return Record{}, fmt.Errorf("prove exact backend stopped before same-generation relaunch: %w", stoppedErr)
				}
				if !stopped.Exists || stopped.State != backend.ObjectStopped {
					return Record{}, fmt.Errorf("exact backend did not stop before same-generation relaunch")
				}
			}
			// A healthy but incomplete owner reuses its validated exchange or
			// performs the one bootstrap below. A poisoned owner was explicitly
			// stopped and relaunches below with the same durable generation.
		default:
			return Record{}, fmt.Errorf("starting session backend state is ambiguous")
		}
	case StateRunning:
		return s.reconcileReady(ctx, record)
	default:
		return Record{}, fmt.Errorf("session %q has incompatible start state %q", name, record.IntendedState)
	}
	if record.StartGeneration == "" || !validUUID(record.StartGeneration) {
		return Record{}, fmt.Errorf("starting session has no valid generation")
	}
	request := supervisor.LaunchRequest{
		Binding:           startBinding(record),
		RuntimeDirectory:  filepath.Join(s.start.RuntimeRoot, string(record.Domain), record.ID, record.StartGeneration),
		HostConfigPath:    s.start.ConfigPath,
		SessionRecordName: string(record.Name),
	}
	// The exact child may need the same session lock to admit managed volume
	// leases. Durable starting intent carries authority across this handoff.
	if err := held.Release(); err != nil {
		return Record{}, fmt.Errorf("release session lock before supervisor launch: %w", err)
	}
	snapshot, err := s.start.Supervisor.StartExact(ctx, request)
	if err != nil {
		return Record{}, fmt.Errorf("start exact generation: %w", err)
	}
	held, err = lock.AcquireSession(ctx, s.domain.StateRoot, string(domainID), string(name))
	if err != nil {
		return Record{}, fmt.Errorf("reacquire session lock after supervisor launch: %w", err)
	}
	current, err := LoadRecord(s.domain.StateRoot, string(domainID), string(name))
	if err != nil {
		return Record{}, fmt.Errorf("reload session after supervisor launch: %w", err)
	}
	if !sameLaunchIdentity(record, current) {
		return Record{}, fmt.Errorf("session launch binding changed during supervisor handoff")
	}
	switch current.IntendedState {
	case StateStarting:
		return s.acceptStarted(current, snapshot)
	case StateRunning:
		return s.reconcileReady(ctx, current)
	default:
		return Record{}, fmt.Errorf("session intent changed to %q during supervisor handoff", current.IntendedState)
	}
}

func sameLaunchIdentity(before, after Record) bool {
	return before.Domain == after.Domain && before.Name == after.Name && before.ID == after.ID &&
		before.Mode == after.Mode && before.Backend == after.Backend &&
		before.GoldenRevision == after.GoldenRevision && before.StartGeneration == after.StartGeneration
}

func (s *Service) reconcileReady(ctx context.Context, record Record) (Record, error) {
	binding := startBinding(record)
	snapshot, err := s.start.Supervisor.Snapshot(ctx, binding)
	if err != nil {
		return Record{}, fmt.Errorf("inspect exact running generation: %w", err)
	}
	return s.persistReady(record, snapshot)
}

func startBinding(record Record) supervisor.Binding {
	return supervisor.Binding{Domain: string(record.Domain), SessionID: record.ID, BackendKind: record.Backend.Kind, BackendObject: record.Backend.ObjectID, Generation: record.StartGeneration}
}

func (s *Service) acceptStarted(record Record, snapshot supervisor.Snapshot) (Record, error) {
	want := startBinding(record)
	now := s.start.Now()
	if snapshot.Binding != want || !snapshot.BackendRunning || !snapshot.SerialHealthy || !snapshot.PinPresent || snapshot.ObservedAt.IsZero() || snapshot.ObservedAt.After(now) || now.Sub(snapshot.ObservedAt) > maxReadySnapshotAge {
		return Record{}, fmt.Errorf("supervisor did not provide a fresh exact bootstrapped snapshot")
	}
	if snapshot.CertificateCurrent && snapshot.ProbeOK && snapshot.ZoneMatches {
		return s.persistReady(record, snapshot)
	}
	return record, nil
}

func (s *Service) persistReady(record Record, snapshot supervisor.Snapshot) (Record, error) {
	want := startBinding(record)
	now := s.start.Now()
	if snapshot.Binding != want || !snapshot.BackendRunning || !snapshot.SerialHealthy || !snapshot.PinPresent || !snapshot.CertificateCurrent || !snapshot.ProbeOK || !snapshot.ZoneMatches || snapshot.ObservedAt.IsZero() || snapshot.ObservedAt.After(now) || now.Sub(snapshot.ObservedAt) > maxReadySnapshotAge {
		return Record{}, fmt.Errorf("supervisor did not provide a fresh exact ready snapshot")
	}
	record.IntendedState = StateRunning
	record.Readiness = ReadinessRecord{Status: ReadinessReady}
	if err := SaveRecord(s.domain.StateRoot, record.Domain, record); err != nil {
		return Record{}, fmt.Errorf("persist running ready session: %w", err)
	}
	return record, nil
}

func (s *Service) validStartDependencies() error {
	if _, err := domain.Parse(string(s.domain.ID)); err != nil || strings.TrimSpace(s.domain.StateRoot) == "" {
		return fmt.Errorf("invalid configured domain")
	}
	if s.observer == nil || s.start.Host == nil || s.start.CA == nil || s.start.Supervisor == nil || s.start.Workspaces == nil || s.start.NewGeneration == nil || s.start.Now == nil || len(s.start.ConfiguredDomains) == 0 || !filepath.IsAbs(s.start.RuntimeRoot) || filepath.Clean(s.start.RuntimeRoot) != s.start.RuntimeRoot || !filepath.IsAbs(s.start.ConfigPath) || filepath.Clean(s.start.ConfigPath) != s.start.ConfigPath {
		return fmt.Errorf("session start dependencies are required")
	}
	return nil
}

func (s *Service) admitStartPrerequisites(ctx context.Context) (RuntimeAdmission, sshx.CAIdentity, error) {
	admission, err := s.start.Host.CheckRuntime(ctx, s.start.HostRequest)
	if err != nil {
		return RuntimeAdmission{}, sshx.CAIdentity{}, fmt.Errorf("admit host runtime: %w", err)
	}
	selected := sshx.Domain{ID: s.domain.ID, StateRoot: s.domain.StateRoot}
	configured := append([]sshx.Domain(nil), s.start.ConfiguredDomains...)
	selectedConfigured := false
	for _, configuredDomain := range configured {
		if configuredDomain == selected {
			selectedConfigured = true
			break
		}
	}
	if !selectedConfigured {
		return RuntimeAdmission{}, sshx.CAIdentity{}, fmt.Errorf("configured domain CA collection omits selected domain")
	}
	ca, err := s.start.CA.Check(ctx, selected, configured)
	if err != nil {
		return RuntimeAdmission{}, sshx.CAIdentity{}, fmt.Errorf("admit selected domain management CA: %w", err)
	}
	return admission, ca, nil
}
