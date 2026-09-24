package session

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/weshofmann/boxwarden/internal/backend"
	"github.com/weshofmann/boxwarden/internal/config"
	"github.com/weshofmann/boxwarden/internal/domain"
	"github.com/weshofmann/boxwarden/internal/golden"
	"github.com/weshofmann/boxwarden/internal/lock"
	"github.com/weshofmann/boxwarden/internal/sshx"
)

// RebuildGate holds the volume-use, session, and storage locks while its
// callback persists candidate intent. The production implementation lives in
// workspacex, which owns attachment and Use records.
type RebuildGate func(context.Context, string, domain.ID, Record, backend.Observer, func() error) error

type RebuildPinLoader interface {
	Load(context.Context, sshx.Binding) (sshx.HostKeyPin, error)
}

type RebuildDependencies struct {
	Observer backend.Observer
	Creator  backend.Creator
	Gate     RebuildGate
	Pins     RebuildPinLoader
}

type RebuildService struct {
	domain config.Domain
	deps   RebuildDependencies
	newID  func() (string, error)
}

func NewRebuildService(configured config.Domain, dependencies RebuildDependencies) *RebuildService {
	return &RebuildService{domain: configured, deps: dependencies, newID: newSessionID}
}

// PrepareCandidate persists one exact replacement intent before cloning. A
// retry may finish a stopped, never-booted candidate from the same journal;
// it never adopts an unrecorded or running backend object.
func (s *RebuildService) PrepareCandidate(ctx context.Context, rawName, revision string) (journal RebuildJournal, err error) {
	if s == nil || s.deps.Observer == nil || s.deps.Creator == nil || s.deps.Gate == nil || s.deps.Pins == nil || s.newID == nil {
		return RebuildJournal{}, fmt.Errorf("rebuild preparation dependencies are required")
	}
	domainID, err := domain.Parse(string(s.domain.ID))
	if err != nil || domainID != s.domain.ID || strings.TrimSpace(s.domain.StateRoot) == "" {
		return RebuildJournal{}, fmt.Errorf("invalid configured rebuild domain")
	}
	name, err := ParseName(rawName)
	if err != nil {
		return RebuildJournal{}, err
	}
	if err := backend.ValidateObjectID(revision); err != nil {
		return RebuildJournal{}, fmt.Errorf("invalid replacement base revision: %w", err)
	}
	transition, err := lock.Acquire(ctx, s.domain.StateRoot, "transition-"+string(domainID)+"-"+string(name))
	if err != nil {
		return RebuildJournal{}, err
	}
	defer func() { err = errors.Join(err, transition.Release()) }()
	record, err := LoadRecord(s.domain.StateRoot, string(domainID), string(name))
	if err != nil {
		return RebuildJournal{}, err
	}
	journal, err = LoadRebuildJournal(s.domain.StateRoot, domainID, string(name))
	if errors.Is(err, os.ErrNotExist) {
		err = s.deps.Gate(ctx, s.domain.StateRoot, domainID, record, s.deps.Observer, func() error {
			var reserveErr error
			journal, reserveErr = s.reserveCandidate(ctx, record, revision)
			return reserveErr
		})
		if err != nil {
			return RebuildJournal{}, err
		}
	} else if err != nil {
		return RebuildJournal{}, err
	}
	if journal.SessionID != record.ID || journal.OldBackend != record.Backend.ObjectID || journal.OldRevision != record.GoldenRevision ||
		journal.CandidateRevision != revision || (journal.Phase != RebuildReserved && journal.Phase != RebuildCloned) {
		return RebuildJournal{}, fmt.Errorf("rebuild journal does not match stopped old system and requested base")
	}
	if record.IntendedState != StateStopped || record.Backend.Kind != "tart" {
		return RebuildJournal{}, fmt.Errorf("old system is not durably stopped for candidate preparation")
	}
	if err := s.verifyOldPin(ctx, journal); err != nil {
		return RebuildJournal{}, err
	}
	oldObservation, err := s.observeExact(ctx, journal.OldBackend)
	if err != nil || !oldObservation.Exists || oldObservation.State != backend.ObjectStopped {
		return RebuildJournal{}, fmt.Errorf("old system is no longer exactly stopped: %v", err)
	}
	selected, err := s.loadStoppedBase(ctx, revision)
	if err != nil {
		return RebuildJournal{}, err
	}
	observation, err := s.observeExact(ctx, journal.CandidateBackend)
	if err != nil {
		return RebuildJournal{}, err
	}
	if journal.Phase == RebuildCloned {
		if !observation.Exists || observation.State != backend.ObjectStopped {
			return RebuildJournal{}, fmt.Errorf("recorded cloned candidate is not stopped")
		}
		return journal, nil
	}
	if !observation.Exists && observation.State == backend.ObjectUnknown {
		if err := s.deps.Creator.Clone(ctx, selected.Backend.ObjectID, journal.CandidateBackend); err != nil {
			return RebuildJournal{}, fmt.Errorf("clone recorded rebuild candidate: %w", err)
		}
		observation, err = s.observeExact(ctx, journal.CandidateBackend)
		if err != nil {
			return RebuildJournal{}, err
		}
	}
	if !observation.Exists || observation.State != backend.ObjectStopped {
		return RebuildJournal{}, fmt.Errorf("recorded rebuild candidate has ambiguous backend state")
	}
	// A stopped candidate in Reserved may be the result of a completed clone
	// followed by a crash. It has never passed a cutover/boot phase, so MAC
	// randomization may be repeated before advancing to Cloned.
	if err := s.deps.Creator.RandomizeMAC(ctx, journal.CandidateBackend); err != nil {
		return RebuildJournal{}, fmt.Errorf("randomize recorded rebuild candidate MAC: %w", err)
	}
	observation, err = s.observeExact(ctx, journal.CandidateBackend)
	if err != nil || !observation.Exists || observation.State != backend.ObjectStopped {
		return RebuildJournal{}, fmt.Errorf("rebuild candidate did not reconcile stopped after MAC randomization: %v", err)
	}
	next := journal
	next.Phase = RebuildCloned
	sessionLock, err := lock.AcquireSession(ctx, s.domain.StateRoot, string(domainID), string(name))
	if err != nil {
		return RebuildJournal{}, err
	}
	defer func() { err = errors.Join(err, sessionLock.Release()) }()
	current, err := LoadRecord(s.domain.StateRoot, string(domainID), string(name))
	if err != nil || current != record {
		return RebuildJournal{}, fmt.Errorf("old session changed before cloned phase: %v", err)
	}
	if err := advanceRebuildJournal(s.domain.StateRoot, journal, next); err != nil {
		return RebuildJournal{}, fmt.Errorf("persist cloned rebuild candidate: %w", err)
	}
	return next, nil
}

func (s *RebuildService) reserveCandidate(ctx context.Context, old Record, revision string) (journal RebuildJournal, err error) {
	held, err := golden.AcquireLock(ctx, s.domain)
	if err != nil {
		return RebuildJournal{}, err
	}
	defer func() { err = errors.Join(err, held.Release()) }()
	selected, err := golden.LoadRevisionLocked(s.domain, revision)
	if err != nil {
		return RebuildJournal{}, err
	}
	if err := s.requireStoppedBase(ctx, selected); err != nil {
		return RebuildJournal{}, err
	}
	operationID, err := s.newID()
	if err != nil || !validUUID(operationID) {
		return RebuildJournal{}, fmt.Errorf("invalid rebuild operation identity: %v", err)
	}
	journal = RebuildJournal{Version: 1, Domain: old.Domain, SessionName: string(old.Name), SessionID: old.ID,
		OperationID: operationID, Phase: RebuildReserved, OldBackend: old.Backend.ObjectID, OldRevision: old.GoldenRevision,
		CandidateBackend: objectIDFor(old.Domain, operationID), CandidateRevision: selected.Revision}
	if err := backend.ValidateObjectID(journal.CandidateBackend); err != nil {
		return RebuildJournal{}, err
	}
	observation, err := s.observeExact(ctx, journal.CandidateBackend)
	if err != nil || observation.Exists || observation.State != backend.ObjectUnknown {
		return RebuildJournal{}, fmt.Errorf("candidate object identity is not absent: %v", err)
	}
	pin, err := s.deps.Pins.Load(ctx, journal.oldPinBinding())
	if err == nil {
		journal.OldPinPresent = true
		raw, marshalErr := json.Marshal(pin)
		if marshalErr != nil {
			return RebuildJournal{}, marshalErr
		}
		digest := sha256.Sum256(raw)
		journal.OldPinDigest = hex.EncodeToString(digest[:])
		if err := journal.verifyOldPinWitness(pin); err != nil {
			return RebuildJournal{}, err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return RebuildJournal{}, fmt.Errorf("load exact old host-key pin: %w", err)
	}
	if err := createRebuildJournal(s.domain.StateRoot, journal); err != nil {
		return RebuildJournal{}, fmt.Errorf("persist rebuild candidate reservation: %w", err)
	}
	return journal, nil
}

func (s *RebuildService) verifyOldPin(ctx context.Context, journal RebuildJournal) error {
	pin, err := s.deps.Pins.Load(ctx, journal.oldPinBinding())
	if errors.Is(err, os.ErrNotExist) && !journal.OldPinPresent {
		return nil
	}
	if err != nil {
		return fmt.Errorf("reload exact old host-key pin: %w", err)
	}
	return journal.verifyOldPinWitness(pin)
}

func (s *RebuildService) loadStoppedBase(ctx context.Context, revision string) (selected golden.Record, err error) {
	held, err := golden.AcquireLock(ctx, s.domain)
	if err != nil {
		return golden.Record{}, err
	}
	defer func() { err = errors.Join(err, held.Release()) }()
	selected, err = golden.LoadRevisionLocked(s.domain, revision)
	if err != nil {
		return golden.Record{}, err
	}
	return selected, s.requireStoppedBase(ctx, selected)
}

func (s *RebuildService) requireStoppedBase(ctx context.Context, selected golden.Record) error {
	if selected.Backend.Kind != "tart" || selected.Backend.ObjectID != selected.Revision {
		return fmt.Errorf("replacement base has invalid backend binding")
	}
	observation, err := s.observeExact(ctx, selected.Backend.ObjectID)
	if err != nil || !observation.Exists || observation.State != backend.ObjectStopped {
		return fmt.Errorf("replacement base must be exact stopped object: %v", err)
	}
	return nil
}

func (s *RebuildService) observeExact(ctx context.Context, objectID string) (backend.Observation, error) {
	observation, err := s.deps.Observer.Observe(ctx, objectID)
	if err != nil || observation.ObjectID != objectID || !observation.State.Valid() ||
		(observation.Exists && observation.State == backend.ObjectUnknown) || (!observation.Exists && observation.State != backend.ObjectUnknown) {
		return backend.Observation{}, fmt.Errorf("ambiguous rebuild backend observation for %q: %v", objectID, err)
	}
	return observation, nil
}
