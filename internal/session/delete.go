package session

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/weshofmann/boxwarden/internal/backend"
	"github.com/weshofmann/boxwarden/internal/config"
	"github.com/weshofmann/boxwarden/internal/domain"
	"github.com/weshofmann/boxwarden/internal/lock"
)

// DeleteGate reserves deletion while holding every attached volume-use lock,
// then the session and storage locks. The callback must persist deleting
// intent before the gate releases them.
type DeleteGate func(context.Context, string, domain.ID, Record, backend.Observer, func() error) error

// DeleteFinalizer detaches only the exact deleted session's idle volumes and
// proves that no attachment remains. It may be retried after a partial write.
type DeleteFinalizer func(context.Context, string, domain.ID, Record, backend.Observer) error

type DeleteDependencies struct {
	Observer backend.Observer
	Deleter  backend.Deleter
	Gate     DeleteGate
	Finalize DeleteFinalizer
}

type DeleteService struct {
	domain config.Domain
	deps   DeleteDependencies
}

func NewDeleteService(configured config.Domain, dependencies DeleteDependencies) *DeleteService {
	return &DeleteService{domain: configured, deps: dependencies}
}

// Delete retires a durably stopped disposable system and retains its separate
// workspace disks. An absent backend is accepted only after deleting intent
// was persisted. An uncertain backend delete remains retryable in that state.
func (s *DeleteService) Delete(ctx context.Context, rawName string) (err error) {
	if s == nil || s.deps.Observer == nil || s.deps.Deleter == nil || s.deps.Gate == nil || s.deps.Finalize == nil {
		return fmt.Errorf("session deletion dependencies are required")
	}
	domainID, err := domain.Parse(string(s.domain.ID))
	if err != nil || domainID != s.domain.ID || strings.TrimSpace(s.domain.StateRoot) == "" {
		return fmt.Errorf("invalid configured deletion domain")
	}
	name, err := ParseName(rawName)
	if err != nil {
		return err
	}
	transition, err := lock.Acquire(ctx, s.domain.StateRoot, "transition-"+string(domainID)+"-"+string(name))
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, transition.Release()) }()
	record, err := LoadRecord(s.domain.StateRoot, string(domainID), string(name))
	if err != nil {
		return err
	}
	if err := RequireNoRebuild(s.domain.StateRoot, domainID, string(name)); err != nil {
		return err
	}
	switch record.IntendedState {
	case StateStopped:
		if err := s.deps.Gate(ctx, s.domain.StateRoot, domainID, record, s.deps.Observer, func() error {
			record.IntendedState = StateDeleting
			record.Readiness = ReadinessRecord{Status: ReadinessNotReady}
			return SaveRecord(s.domain.StateRoot, domainID, record)
		}); err != nil {
			return fmt.Errorf("reserve stopped session deletion: %w", err)
		}
	case StateDeleting:
		// A prior atomic rename may have succeeded even if its directory sync
		// failed. Re-persist the exact marker before any backend mutation.
		sessionLock, lockErr := lock.AcquireSession(ctx, s.domain.StateRoot, string(domainID), string(name))
		if lockErr != nil {
			return lockErr
		}
		current, loadErr := LoadRecord(s.domain.StateRoot, string(domainID), string(name))
		if loadErr != nil || current != record {
			return errors.Join(fmt.Errorf("deleting session changed before retry: %v", loadErr), sessionLock.Release())
		}
		syncErr := SaveRecord(s.domain.StateRoot, domainID, record)
		if releaseErr := sessionLock.Release(); releaseErr != nil {
			syncErr = errors.Join(syncErr, releaseErr)
		}
		if syncErr != nil {
			return fmt.Errorf("sync deletion intent before retry: %w", syncErr)
		}
	default:
		return fmt.Errorf("session is not durably stopped or deleting")
	}
	observation, err := s.deps.Observer.Observe(ctx, record.Backend.ObjectID)
	if err != nil || observation.ObjectID != record.Backend.ObjectID || !observation.State.Valid() ||
		(observation.Exists && observation.State != backend.ObjectStopped) ||
		(!observation.Exists && observation.State != backend.ObjectUnknown) {
		return fmt.Errorf("ambiguous backend state before session deletion: %v", err)
	}
	if observation.Exists {
		if err := s.deps.Deleter.Delete(ctx, record.Backend.ObjectID); err != nil {
			return fmt.Errorf("delete exact stopped backend: %w", err)
		}
	}
	observation, err = s.deps.Observer.Observe(ctx, record.Backend.ObjectID)
	if err != nil || observation.ObjectID != record.Backend.ObjectID || observation.Exists || observation.State != backend.ObjectUnknown {
		return fmt.Errorf("backend did not reconcile absent after deletion: %v", err)
	}
	if err := s.deps.Finalize(ctx, s.domain.StateRoot, domainID, record, s.deps.Observer); err != nil {
		return fmt.Errorf("retain workspace volumes after backend deletion: %w", err)
	}
	return nil
}
