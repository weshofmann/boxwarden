package session

import (
	"context"
	"fmt"
	"strings"

	"github.com/weshofmann/boxwarden/internal/backend"
	"github.com/weshofmann/boxwarden/internal/domain"
	"github.com/weshofmann/boxwarden/internal/lock"
)

// Stop records the exact generation being stopped before contacting its live
// supervisor. A failed or ambiguous stop retains that generation as stopping;
// it cannot become a new start or a stopped volume handoff by assertion.
func (s *Service) Stop(ctx context.Context, rawName string) (record Record, err error) {
	if s == nil || s.start == nil || s.observer == nil || s.start.Supervisor == nil || s.start.Workspaces == nil {
		return Record{}, fmt.Errorf("session stop dependencies are required")
	}
	domainID, err := domain.Parse(string(s.domain.ID))
	if err != nil || domainID != s.domain.ID || strings.TrimSpace(s.domain.StateRoot) == "" {
		return Record{}, fmt.Errorf("invalid configured domain")
	}
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
	record, err = LoadRecord(s.domain.StateRoot, string(domainID), string(name))
	if err != nil {
		return Record{}, fmt.Errorf("load session record: %w", err)
	}
	if record.Backend.Kind != "tart" || record.Backend.ObjectID == "" {
		return Record{}, fmt.Errorf("unsupported session backend binding")
	}
	observation, err := s.observeExact(ctx, record.Backend.ObjectID)
	if err != nil {
		return Record{}, err
	}
	if !observation.Exists || observation.State != backend.ObjectStopped && observation.State != backend.ObjectRunning {
		return Record{}, fmt.Errorf("exact session backend is absent or ambiguous")
	}
	if record.IntendedState == StateStopped {
		if observation.State != backend.ObjectStopped {
			return Record{}, fmt.Errorf("stopped intent conflicts with running backend")
		}
		return s.finishStopped(ctx, domainID, name, record, &held)
	}
	if record.IntendedState != StateStarting && record.IntendedState != StateRunning && record.IntendedState != StateStopping {
		return Record{}, fmt.Errorf("session %q has incompatible stop state %q", name, record.IntendedState)
	}
	if !validUUID(record.StartGeneration) {
		return Record{}, fmt.Errorf("session has no exact generation to stop")
	}
	if record.IntendedState == StateStopping && observation.State == backend.ObjectStopped {
		quiesced, proofErr := s.start.Supervisor.Quiesced(ctx, startBinding(record))
		if proofErr != nil {
			return Record{}, fmt.Errorf("prove exact generation quiesced: %w", proofErr)
		}
		if quiesced {
			return s.finishStopped(ctx, domainID, name, record, &held)
		}
	}
	if record.IntendedState != StateStopping {
		record.IntendedState = StateStopping
		record.Readiness = ReadinessRecord{Status: ReadinessNotReady}
		if err := SaveRecord(s.domain.StateRoot, domainID, record); err != nil {
			return Record{}, fmt.Errorf("persist stopping intent: %w", err)
		}
	}
	// The owner may need the session lock while ending a volume lease. The
	// persisted stopping intent prevents a new generation during this wait.
	if err := held.Release(); err != nil {
		return Record{}, fmt.Errorf("release session lock before supervisor stop: %w", err)
	}
	if err := s.start.Supervisor.Stop(ctx, startBinding(record)); err != nil {
		return Record{}, fmt.Errorf("stop exact generation: %w", err)
	}
	return s.finishStopped(ctx, domainID, name, record, &held)
}

// finishStopped releases the session lock before the volume-first batch, then
// reacquires it to publish Stopped only after exact Use release. The caller
// already proved stop/wait/reap for Stopping, or has durable Stopped intent
// with no launch commit to reconcile.
func (s *Service) finishStopped(ctx context.Context, domainID domain.ID, name Name, expected Record, held **lock.Held) (Record, error) {
	if err := (*held).Release(); err != nil {
		return Record{}, fmt.Errorf("release session lock before workspace use release: %w", err)
	}
	if err := s.start.Workspaces.ReleaseUses(ctx, s.domain.StateRoot, domainID, expected, s.observer); err != nil {
		return Record{}, fmt.Errorf("release exact workspace uses after stop: %w", err)
	}
	var err error
	*held, err = lock.AcquireSession(ctx, s.domain.StateRoot, string(domainID), string(name))
	if err != nil {
		return Record{}, fmt.Errorf("reacquire session lock after workspace use release: %w", err)
	}
	current, err := LoadRecord(s.domain.StateRoot, string(domainID), string(name))
	if err != nil {
		return Record{}, fmt.Errorf("reload session after workspace use release: %w", err)
	}
	if current != expected {
		return Record{}, fmt.Errorf("session changed during workspace use release")
	}
	observation, err := s.observeExact(ctx, current.Backend.ObjectID)
	if err != nil {
		return Record{}, fmt.Errorf("observe exact backend after stop: %w", err)
	}
	if !observation.Exists || observation.State != backend.ObjectStopped {
		return Record{}, fmt.Errorf("supervisor stop did not prove exact backend stopped")
	}
	if current.IntendedState == StateStopped {
		return current, nil
	}
	return s.persistStopped(current)
}

func sameStoppedIdentity(before, after Record) bool {
	return before.Domain == after.Domain && before.Name == after.Name && before.ID == after.ID &&
		before.Mode == after.Mode && before.Backend == after.Backend && before.GoldenRevision == after.GoldenRevision
}

func (s *Service) persistStopped(record Record) (Record, error) {
	record.IntendedState = StateStopped
	record.StartGeneration = ""
	record.Readiness = ReadinessRecord{Status: ReadinessNotReady}
	if err := SaveRecord(s.domain.StateRoot, record.Domain, record); err != nil {
		return Record{}, fmt.Errorf("persist stopped session: %w", err)
	}
	return record, nil
}
