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

// RuntimeAdmission is hostx's public projection. Session cannot access its
// opaque Screen capability; the future child composition owns that boundary.
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
	Stop(context.Context, supervisor.Binding) error
}

type StartDependencies struct {
	Observer          backend.Observer
	Host              RuntimeChecker
	HostRequest       hostx.Request
	CA                CAValidator
	ConfiguredDomains []sshx.Domain
	Supervisor        SupervisorControl
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
	held, err := lock.AcquireSession(ctx, s.domain.StateRoot, string(domainID), string(name))
	if err != nil {
		return Record{}, fmt.Errorf("acquire session lock: %w", err)
	}
	defer func() {
		if releaseErr := held.Release(); err == nil && releaseErr != nil {
			err = fmt.Errorf("release session lock: %w", releaseErr)
		}
	}()

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

	switch record.IntendedState {
	case StateStopped:
		observation, observeErr := s.observer.Observe(ctx, record.Backend.ObjectID)
		if observeErr != nil {
			return Record{}, fmt.Errorf("observe stopped session backend: %w", observeErr)
		}
		if !observation.Exists || observation.State != backend.ObjectStopped {
			return Record{}, fmt.Errorf("stopped session does not match one stopped backend object")
		}
		generation, generationErr := s.start.NewGeneration()
		if generationErr != nil || !validUUID(generation) {
			return Record{}, fmt.Errorf("generate start generation: %w", generationErr)
		}
		record.IntendedState = StateStarting
		record.StartGeneration = generation
		record.Readiness = ReadinessRecord{Status: ReadinessStarting}
		if saveErr := SaveRecord(s.domain.StateRoot, domainID, record); saveErr != nil {
			return Record{}, fmt.Errorf("persist starting session: %w", saveErr)
		}
	case StateStarting:
		// The existing durable generation is the only retry target.
	case StateRunning:
		return s.reconcileReady(ctx, record)
	default:
		return Record{}, fmt.Errorf("session %q has incompatible start state %q", name, record.IntendedState)
	}
	if record.StartGeneration == "" || !validUUID(record.StartGeneration) {
		return Record{}, fmt.Errorf("starting session has no valid generation")
	}
	request := supervisor.LaunchRequest{
		Binding:           supervisor.Binding{Domain: string(record.Domain), SessionID: record.ID, BackendKind: record.Backend.Kind, BackendObject: record.Backend.ObjectID, Generation: record.StartGeneration},
		RuntimeDirectory:  filepath.Join(s.start.RuntimeRoot, string(record.Domain), record.ID, record.StartGeneration),
		HostConfigPath:    s.start.ConfigPath,
		SessionRecordName: string(record.Name),
	}
	snapshot, err := s.start.Supervisor.StartExact(ctx, request)
	if err != nil {
		return Record{}, fmt.Errorf("start exact generation: %w", err)
	}
	return s.persistReady(record, snapshot)
}

func (s *Service) reconcileReady(ctx context.Context, record Record) (Record, error) {
	binding := supervisor.Binding{Domain: string(record.Domain), SessionID: record.ID, BackendKind: record.Backend.Kind, BackendObject: record.Backend.ObjectID, Generation: record.StartGeneration}
	snapshot, err := s.start.Supervisor.Snapshot(ctx, binding)
	if err != nil {
		return Record{}, fmt.Errorf("inspect exact running generation: %w", err)
	}
	return s.persistReady(record, snapshot)
}

func (s *Service) persistReady(record Record, snapshot supervisor.Snapshot) (Record, error) {
	want := supervisor.Binding{Domain: string(record.Domain), SessionID: record.ID, BackendKind: record.Backend.Kind, BackendObject: record.Backend.ObjectID, Generation: record.StartGeneration}
	now := s.start.Now()
	if snapshot.Binding != want || !snapshot.BackendRunning || !snapshot.BrokerHealthy || !snapshot.ScreenHealthy || !snapshot.PinPresent || !snapshot.CertificateCurrent || !snapshot.ProbeOK || !snapshot.ZoneMatches || snapshot.ObservedAt.IsZero() || snapshot.ObservedAt.After(now) || now.Sub(snapshot.ObservedAt) > maxReadySnapshotAge {
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
	if s.observer == nil || s.start.Host == nil || s.start.CA == nil || s.start.Supervisor == nil || s.start.NewGeneration == nil || s.start.Now == nil || len(s.start.ConfiguredDomains) == 0 || !filepath.IsAbs(s.start.RuntimeRoot) || filepath.Clean(s.start.RuntimeRoot) != s.start.RuntimeRoot || !filepath.IsAbs(s.start.ConfigPath) || filepath.Clean(s.start.ConfigPath) != s.start.ConfigPath {
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
