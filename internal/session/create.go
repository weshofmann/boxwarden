package session

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/weshofmann/boxwarden/internal/backend"
	"github.com/weshofmann/boxwarden/internal/config"
	"github.com/weshofmann/boxwarden/internal/domain"
	"github.com/weshofmann/boxwarden/internal/golden"
	"github.com/weshofmann/boxwarden/internal/lock"
)

type createStage string

const (
	createAfterIntent createStage = "after_intent"
	createAfterClone  createStage = "after_clone"
	createAfterMAC    createStage = "after_mac"
)

type createHook func(createStage, Record) error

// Service owns domain-scoped session state transitions and delegates only
// backend observation and the two V2 creation mutations through narrow seams.
type Service struct {
	domain   config.Domain
	observer backend.Observer
	creator  backend.Creator
	newID    func() (string, error)
	hook     createHook
}

// NewService composes a domain-scoped session service. Dependencies are
// validated again at the operation boundary before state or backend access.
func NewService(configured config.Domain, observer backend.Observer, creator backend.Creator) *Service {
	return &Service{
		domain:   configured,
		observer: observer,
		creator:  creator,
		newID:    newSessionID,
	}
}

// Create reserves and reconciles one stopped disposable session. The session
// lock is held across observation, state persistence, and backend mutation so
// retries for the same name cannot race one another.
func (s *Service) Create(ctx context.Context, rawName string, mode Mode) (record Record, err error) {
	if s == nil {
		return Record{}, fmt.Errorf("session service is required")
	}
	domainID, err := domain.Parse(string(s.domain.ID))
	if err != nil || domainID != s.domain.ID || strings.TrimSpace(s.domain.StateRoot) == "" {
		return Record{}, fmt.Errorf("invalid configured domain")
	}
	if s.observer == nil || s.creator == nil {
		return Record{}, fmt.Errorf("session observer and creator are required")
	}
	name, err := ParseName(rawName)
	if err != nil {
		return Record{}, err
	}
	if mode != ModeClean && mode != ModeQuarantine {
		return Record{}, fmt.Errorf("invalid session mode %q", mode)
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
		if !errors.Is(err, os.ErrNotExist) {
			return Record{}, fmt.Errorf("load existing session intent: %w", err)
		}
		record, err = s.reserveIntent(ctx, domainID, name, mode)
		if err != nil {
			return Record{}, err
		}
		if err := s.runHook(createAfterIntent, record); err != nil {
			return Record{}, err
		}
	} else {
		if record.Mode != mode {
			return Record{}, fmt.Errorf("session %q already exists with mode %q", name, record.Mode)
		}
		switch record.IntendedState {
		case StateStopped:
			observation, err := s.observeExact(ctx, record.Backend.ObjectID)
			if err != nil {
				return Record{}, err
			}
			if !observation.Exists || observation.State != backend.ObjectStopped {
				return Record{}, fmt.Errorf("session %q stopped intent does not match one stopped backend object", name)
			}
			if err := SaveRecord(s.domain.StateRoot, record.Domain, record); err != nil {
				return Record{}, fmt.Errorf("re-persist stopped session: %w", err)
			}
			record, err = LoadRecord(s.domain.StateRoot, string(domainID), string(name))
			if err != nil {
				return Record{}, fmt.Errorf("reload persisted stopped session: %w", err)
			}
			return record, nil
		case StateCreating:
			if err := validateCreatingIdentity(record); err != nil {
				return Record{}, err
			}
			// A prior atomic rename may have become visible before its directory
			// fsync failed. Rewriting the exact intent closes that durability gap
			// before any retry is allowed to mutate the backend.
			if err := SaveRecord(s.domain.StateRoot, record.Domain, record); err != nil {
				return Record{}, fmt.Errorf("re-persist creating intent: %w", err)
			}
		default:
			return Record{}, fmt.Errorf("session %q already exists with intended state %q", name, record.IntendedState)
		}
	}

	return s.reconcileCreating(ctx, record)
}

func (s *Service) reserveIntent(ctx context.Context, domainID domain.ID, name Name, mode Mode) (record Record, err error) {
	held, err := golden.AcquireLock(ctx, s.domain)
	if err != nil {
		return Record{}, fmt.Errorf("acquire golden while reserving session: %w", err)
	}
	defer func() {
		if releaseErr := held.Release(); err == nil && releaseErr != nil {
			err = fmt.Errorf("release golden lock: %w", releaseErr)
		}
	}()

	selected, err := golden.LoadCurrentLocked(s.domain)
	if err != nil {
		return Record{}, fmt.Errorf("resolve current golden: %w", err)
	}
	if err := s.requireStoppedGolden(ctx, selected); err != nil {
		return Record{}, err
	}
	if s.newID == nil {
		return Record{}, fmt.Errorf("session identity source is required")
	}
	id, err := s.newID()
	if err != nil {
		return Record{}, err
	}
	if !validUUID(id) {
		return Record{}, fmt.Errorf("generated invalid session identity")
	}
	objectID := objectIDFor(domainID, id)
	if err := backend.ValidateObjectID(objectID); err != nil {
		return Record{}, fmt.Errorf("derive backend object identity: %w", err)
	}
	if err := requireUnreservedBackendObject(s.domain.StateRoot, domainID, name, objectID); err != nil {
		return Record{}, fmt.Errorf("reserve backend object identity: %w", err)
	}
	observation, err := s.observeExact(ctx, objectID)
	if err != nil {
		return Record{}, err
	}
	if observation.Exists || observation.State != backend.ObjectUnknown {
		return Record{}, fmt.Errorf("backend object identity collision for reserved session %q", name)
	}

	record = Record{
		Version:        recordVersion,
		Domain:         domainID,
		Name:           name,
		ID:             id,
		Mode:           mode,
		IntendedState:  StateCreating,
		Backend:        BackendRef{Kind: "tart", ObjectID: objectID},
		GoldenRevision: selected.Revision,
		Readiness:      ReadinessRecord{Status: ReadinessNotReady},
	}
	if err := SaveRecord(s.domain.StateRoot, domainID, record); err != nil {
		return Record{}, fmt.Errorf("persist creating intent: %w", err)
	}
	return record, nil
}

func (s *Service) reconcileCreating(ctx context.Context, record Record) (Record, error) {
	observation, err := s.observeExact(ctx, record.Backend.ObjectID)
	if err != nil {
		return Record{}, err
	}
	switch {
	case !observation.Exists && observation.State == backend.ObjectUnknown:
		selected, err := s.loadStoppedRevision(ctx, record.GoldenRevision)
		if err != nil {
			return Record{}, err
		}
		if err := s.creator.Clone(ctx, selected.Backend.ObjectID, record.Backend.ObjectID); err != nil {
			return Record{}, fmt.Errorf("clone session backend object: %w", err)
		}
	case observation.Exists && observation.State == backend.ObjectStopped:
		// A prior attempt completed the clone before it was able to advance
		// durable intent. Reuse the exact recorded target.
	default:
		return Record{}, fmt.Errorf("creating session %q has ambiguous backend state", record.Name)
	}
	if err := s.runHook(createAfterClone, record); err != nil {
		return Record{}, err
	}
	if err := s.creator.RandomizeMAC(ctx, record.Backend.ObjectID); err != nil {
		return Record{}, fmt.Errorf("randomize session MAC: %w", err)
	}
	if err := s.runHook(createAfterMAC, record); err != nil {
		return Record{}, err
	}
	observation, err = s.observeExact(ctx, record.Backend.ObjectID)
	if err != nil {
		return Record{}, err
	}
	if !observation.Exists || observation.State != backend.ObjectStopped {
		return Record{}, fmt.Errorf("created session %q did not reconcile to one stopped backend object", record.Name)
	}
	record.IntendedState = StateStopped
	if err := SaveRecord(s.domain.StateRoot, record.Domain, record); err != nil {
		return Record{}, fmt.Errorf("persist stopped session: %w", err)
	}
	return record, nil
}

func (s *Service) loadStoppedRevision(ctx context.Context, revision string) (selected golden.Record, err error) {
	held, err := golden.AcquireLock(ctx, s.domain)
	if err != nil {
		return golden.Record{}, fmt.Errorf("acquire golden while reconciling session: %w", err)
	}
	defer func() {
		if releaseErr := held.Release(); err == nil && releaseErr != nil {
			err = fmt.Errorf("release golden lock: %w", releaseErr)
		}
	}()
	selected, err = golden.LoadRevisionLocked(s.domain, revision)
	if err != nil {
		return golden.Record{}, fmt.Errorf("resolve recorded golden revision %q: %w", revision, err)
	}
	if err := s.requireStoppedGolden(ctx, selected); err != nil {
		return golden.Record{}, err
	}
	return selected, nil
}

func (s *Service) requireStoppedGolden(ctx context.Context, selected golden.Record) error {
	if selected.Backend.Kind != "tart" || selected.Backend.ObjectID != selected.Revision {
		return fmt.Errorf("registered golden %q has an invalid backend reference", selected.Revision)
	}
	observation, err := s.observeExact(ctx, selected.Backend.ObjectID)
	if err != nil {
		return fmt.Errorf("observe registered golden %q: %w", selected.Revision, err)
	}
	if !observation.Exists || observation.State != backend.ObjectStopped {
		return fmt.Errorf("registered golden %q must still be one stopped backend object", selected.Revision)
	}
	return nil
}

func (s *Service) observeExact(ctx context.Context, objectID string) (backend.Observation, error) {
	observation, err := s.observer.Observe(ctx, objectID)
	if err != nil {
		return backend.Observation{}, fmt.Errorf("observe backend object %q: %w", objectID, err)
	}
	if observation.ObjectID != objectID || !observation.State.Valid() ||
		(observation.Exists && observation.State == backend.ObjectUnknown) ||
		(!observation.Exists && observation.State != backend.ObjectUnknown) {
		return backend.Observation{}, fmt.Errorf("backend returned ambiguous observation for %q", objectID)
	}
	return observation, nil
}

func (s *Service) runHook(stage createStage, record Record) error {
	if s.hook == nil {
		return nil
	}
	if err := s.hook(stage, record); err != nil {
		return fmt.Errorf("create interrupted %s: %w", stage, err)
	}
	return nil
}

func validateCreatingIdentity(record Record) error {
	if record.GoldenRevision == "" || record.Backend.Kind != "tart" {
		return fmt.Errorf("creating session %q has incomplete immutable intent", record.Name)
	}
	want := objectIDFor(record.Domain, record.ID)
	if record.Backend.ObjectID != want {
		return fmt.Errorf("creating session %q backend identity does not match its UUID reservation", record.Name)
	}
	return nil
}

func objectIDFor(domainID domain.ID, id string) string {
	return "boxwarden-" + string(domainID) + "-" + strings.ReplaceAll(id, "-", "")
}
