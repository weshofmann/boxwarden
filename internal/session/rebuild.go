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
	Deleter  backend.Deleter
	Gate     RebuildGate
	Pins     RebuildPinLoader
}

type RebuildService struct {
	domain      config.Domain
	deps        RebuildDependencies
	newID       func() (string, error)
	cutoverHook func() error
	readyHook   func() error
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

// Cutover switches only the system backend and base revision after the
// never-booted candidate has reconciled to stopped. The journal advances
// first, so a crash between the two atomic files is an explicit retry state.
// No candidate launch is authorized until both journal and session record
// identify the same candidate.
func (s *RebuildService) Cutover(ctx context.Context, rawName string) (record Record, err error) {
	if s == nil || s.deps.Observer == nil || s.deps.Pins == nil {
		return Record{}, fmt.Errorf("rebuild cutover dependencies are required")
	}
	domainID, err := domain.Parse(string(s.domain.ID))
	if err != nil || domainID != s.domain.ID || strings.TrimSpace(s.domain.StateRoot) == "" {
		return Record{}, fmt.Errorf("invalid configured rebuild domain")
	}
	name, err := ParseName(rawName)
	if err != nil {
		return Record{}, err
	}
	transition, err := lock.Acquire(ctx, s.domain.StateRoot, "transition-"+string(domainID)+"-"+string(name))
	if err != nil {
		return Record{}, err
	}
	defer func() { err = errors.Join(err, transition.Release()) }()
	held, err := lock.AcquireSession(ctx, s.domain.StateRoot, string(domainID), string(name))
	if err != nil {
		return Record{}, err
	}
	defer func() { err = errors.Join(err, held.Release()) }()
	journal, err := LoadRebuildJournal(s.domain.StateRoot, domainID, string(name))
	if err != nil || (journal.Phase != RebuildCloned && journal.Phase != RebuildCutover) {
		return Record{}, fmt.Errorf("candidate is not journaled as cloned or in cutover: %v", err)
	}
	record, err = LoadRecord(s.domain.StateRoot, string(domainID), string(name))
	if err != nil || record.ID != journal.SessionID || record.Backend.Kind != "tart" {
		return Record{}, fmt.Errorf("session identity changed before rebuild cutover: %v", err)
	}
	if journal.Phase == RebuildCutover && record.Backend.ObjectID == journal.CandidateBackend && record.GoldenRevision == journal.CandidateRevision {
		return record, nil
	}
	if record.IntendedState != StateStopped || record.Backend.ObjectID != journal.OldBackend || record.GoldenRevision != journal.OldRevision || record.StartGeneration != "" {
		return Record{}, fmt.Errorf("old stopped system does not match rebuild cutover journal")
	}
	if err := s.verifyOldPin(ctx, journal); err != nil {
		return Record{}, err
	}
	for _, objectID := range []string{journal.OldBackend, journal.CandidateBackend} {
		observation, observeErr := s.observeExact(ctx, objectID)
		if observeErr != nil || !observation.Exists || observation.State != backend.ObjectStopped {
			return Record{}, fmt.Errorf("rebuild cutover requires exact stopped object %q: %v", objectID, observeErr)
		}
	}
	if journal.Phase == RebuildCloned {
		next := journal
		next.Phase = RebuildCutover
		if err := advanceRebuildJournal(s.domain.StateRoot, journal, next); err != nil {
			return Record{}, fmt.Errorf("persist rebuild cutover intent: %w", err)
		}
		journal = next
		if s.cutoverHook != nil {
			if err := s.cutoverHook(); err != nil {
				return Record{}, err
			}
		}
	}
	record.Backend.ObjectID = journal.CandidateBackend
	record.GoldenRevision = journal.CandidateRevision
	if err := SaveRecord(s.domain.StateRoot, domainID, record); err != nil {
		return Record{}, fmt.Errorf("persist candidate as active system: %w", err)
	}
	return record, nil
}

// ConfirmReady advances the rebuild only after a fresh exact-generation owner
// snapshot and durable workspace Use verification. It does not infer READY
// from the stored readiness bit or backend listing alone.
func (s *RebuildService) ConfirmReady(ctx context.Context, rawName string, starter *Service) (record Record, err error) {
	if s == nil || starter == nil || starter.start == nil || starter.domain != s.domain {
		return Record{}, fmt.Errorf("exact rebuild starter is required")
	}
	if err := starter.validStartDependencies(); err != nil {
		return Record{}, err
	}
	domainID, err := domain.Parse(string(s.domain.ID))
	if err != nil || domainID != s.domain.ID {
		return Record{}, fmt.Errorf("invalid rebuild domain")
	}
	name, err := ParseName(rawName)
	if err != nil {
		return Record{}, err
	}
	transition, err := lock.Acquire(ctx, s.domain.StateRoot, "transition-"+string(domainID)+"-"+string(name))
	if err != nil {
		return Record{}, err
	}
	defer func() { err = errors.Join(err, transition.Release()) }()
	held, err := lock.AcquireSession(ctx, s.domain.StateRoot, string(domainID), string(name))
	if err != nil {
		return Record{}, err
	}
	defer func() { err = errors.Join(err, held.Release()) }()
	journal, err := LoadRebuildJournal(s.domain.StateRoot, domainID, string(name))
	if err != nil || (journal.Phase != RebuildCutover && journal.Phase != RebuildReady) {
		return Record{}, fmt.Errorf("rebuild is not in candidate readiness phase: %v", err)
	}
	record, err = LoadRecord(s.domain.StateRoot, string(domainID), string(name))
	if err != nil || record.ID != journal.SessionID || record.Backend.Kind != "tart" ||
		record.Backend.ObjectID != journal.CandidateBackend || record.GoldenRevision != journal.CandidateRevision ||
		record.IntendedState != StateRunning || record.Readiness.Status != ReadinessReady {
		return Record{}, fmt.Errorf("candidate session is not durably ready: %v", err)
	}
	if err := starter.start.Workspaces.VerifyUses(ctx, s.domain.StateRoot, domainID, record); err != nil {
		return Record{}, fmt.Errorf("verify candidate workspace uses before rebuild ready: %w", err)
	}
	record, err = starter.reconcileReady(ctx, record)
	if err != nil {
		return Record{}, fmt.Errorf("verify fresh exact candidate readiness: %w", err)
	}
	if journal.Phase == RebuildReady {
		return record, nil
	}
	if s.readyHook != nil {
		if err := s.readyHook(); err != nil {
			return Record{}, err
		}
	}
	next := journal
	next.Phase = RebuildReady
	if err := advanceRebuildJournal(s.domain.StateRoot, journal, next); err != nil {
		return Record{}, fmt.Errorf("persist rebuild ready phase: %w", err)
	}
	return record, nil
}

// RetireOld records exact deletion intent before touching the old backend.
// An absent old object is accepted only in Retiring, after which the journal
// is removed last. The candidate remains the active session throughout.
func (s *RebuildService) RetireOld(ctx context.Context, rawName string, starter *Service) (record Record, err error) {
	if s == nil || s.deps.Observer == nil || s.deps.Deleter == nil || starter == nil || starter.start == nil || starter.domain != s.domain {
		return Record{}, fmt.Errorf("exact rebuild retirement dependencies are required")
	}
	if err := starter.validStartDependencies(); err != nil {
		return Record{}, err
	}
	domainID, err := domain.Parse(string(s.domain.ID))
	if err != nil || domainID != s.domain.ID {
		return Record{}, fmt.Errorf("invalid rebuild domain")
	}
	name, err := ParseName(rawName)
	if err != nil {
		return Record{}, err
	}
	transition, err := lock.Acquire(ctx, s.domain.StateRoot, "transition-"+string(domainID)+"-"+string(name))
	if err != nil {
		return Record{}, err
	}
	defer func() { err = errors.Join(err, transition.Release()) }()
	held, err := lock.AcquireSession(ctx, s.domain.StateRoot, string(domainID), string(name))
	if err != nil {
		return Record{}, err
	}
	defer func() {
		if held != nil {
			err = errors.Join(err, held.Release())
		}
	}()
	journal, err := LoadRebuildJournal(s.domain.StateRoot, domainID, string(name))
	if err != nil || (journal.Phase != RebuildReady && journal.Phase != RebuildRetiring) {
		return Record{}, fmt.Errorf("rebuild is not ready for exact old-system retirement: %v", err)
	}
	record, err = LoadRecord(s.domain.StateRoot, string(domainID), string(name))
	if err != nil || record.ID != journal.SessionID || record.Backend.Kind != "tart" ||
		record.Backend.ObjectID != journal.CandidateBackend || record.GoldenRevision != journal.CandidateRevision ||
		record.IntendedState != StateRunning || record.Readiness.Status != ReadinessReady {
		return Record{}, fmt.Errorf("active candidate changed before retirement: %v", err)
	}
	if err := starter.start.Workspaces.VerifyUses(ctx, s.domain.StateRoot, domainID, record); err != nil {
		return Record{}, fmt.Errorf("verify active candidate workspace uses before retirement: %w", err)
	}
	record, err = starter.reconcileReady(ctx, record)
	if err != nil {
		return Record{}, fmt.Errorf("require fresh candidate READY before old-system retirement: %w", err)
	}
	old, err := s.observeExact(ctx, journal.OldBackend)
	if err != nil || (old.Exists && old.State != backend.ObjectStopped) {
		return Record{}, fmt.Errorf("old rebuild system is not provably stopped or absent: %v", err)
	}
	if journal.Phase == RebuildReady {
		if !old.Exists {
			return Record{}, fmt.Errorf("old rebuild system disappeared before deletion intent")
		}
		next := journal
		next.Phase = RebuildRetiring
		if err := advanceRebuildJournal(s.domain.StateRoot, journal, next); err != nil {
			return Record{}, fmt.Errorf("persist old-system retirement intent: %w", err)
		}
		journal = next
	}
	if err := held.Release(); err != nil {
		return Record{}, err
	}
	held = nil
	if old.Exists {
		if err := s.deps.Deleter.Delete(ctx, journal.OldBackend); err != nil {
			return Record{}, fmt.Errorf("delete exact old rebuild object: %w", err)
		}
	}
	old, err = s.observeExact(ctx, journal.OldBackend)
	if err != nil || old.Exists || old.State != backend.ObjectUnknown {
		return Record{}, fmt.Errorf("old rebuild object did not reconcile absent: %v", err)
	}
	held, err = lock.AcquireSession(ctx, s.domain.StateRoot, string(domainID), string(name))
	if err != nil {
		return Record{}, err
	}
	current, err := LoadRecord(s.domain.StateRoot, string(domainID), string(name))
	if err != nil || current != record {
		return Record{}, fmt.Errorf("active candidate changed during old-system deletion: %v", err)
	}
	if err := removeRebuildJournal(s.domain.StateRoot, journal); err != nil {
		return Record{}, fmt.Errorf("clear completed rebuild journal: %w", err)
	}
	return record, nil
}

// Execute advances one requested replacement through its durable journal.
// Every phase is retryable; an incomplete boot remains Starting and needs a
// later invocation. An already selected base with no journal is a no-op, so
// retrying a completed command does not create another disposable system.
func (s *RebuildService) Execute(ctx context.Context, rawName, revision string, starter *Service) (Record, error) {
	if s == nil || starter == nil || starter.domain != s.domain {
		return Record{}, fmt.Errorf("matching rebuild starter is required")
	}
	name, err := ParseName(rawName)
	if err != nil {
		return Record{}, err
	}
	current, err := LoadRecord(s.domain.StateRoot, string(s.domain.ID), string(name))
	if err != nil {
		return Record{}, err
	}
	j, err := LoadRebuildJournal(s.domain.StateRoot, s.domain.ID, string(name))
	if errors.Is(err, os.ErrNotExist) {
		if revision == "" {
			return Record{}, fmt.Errorf("new rebuild requires an exact prepared base revision")
		}
		if current.GoldenRevision == revision {
			if err := syncRebuildJournalRegistry(s.domain.StateRoot); err != nil {
				return Record{}, fmt.Errorf("settle completed rebuild journal directory: %w", err)
			}
			if current.IntendedState != StateStopped && (current.IntendedState != StateRunning || current.Readiness.Status != ReadinessReady) {
				return Record{}, fmt.Errorf("session already uses requested base but is not stopped or ready")
			}
			if current.IntendedState == StateRunning {
				return starter.Start(ctx, string(name)) // fresh exact owner evidence
			}
			observation, observeErr := s.observeExact(ctx, current.Backend.ObjectID)
			if observeErr != nil || !observation.Exists || observation.State != backend.ObjectStopped {
				return Record{}, fmt.Errorf("same-base stopped session is not exactly stopped: %v", observeErr)
			}
			return current, nil
		}
	} else if err != nil {
		return Record{}, err
	} else {
		if revision == "" {
			revision = j.CandidateRevision
		}
		if revision != j.CandidateRevision {
			return Record{}, fmt.Errorf("pending rebuild is bound to base %q", j.CandidateRevision)
		}
	}
	if err := backend.ValidateObjectID(revision); err != nil {
		return Record{}, err
	}
	if errors.Is(err, os.ErrNotExist) || j.Phase == RebuildReserved || j.Phase == RebuildCloned {
		j, err = s.PrepareCandidate(ctx, string(name), revision)
		if err != nil {
			return Record{}, err
		}
	}
	if j.Phase == RebuildCloned || j.Phase == RebuildCutover {
		if _, err := s.Cutover(ctx, string(name)); err != nil {
			return Record{}, err
		}
		j, err = LoadRebuildJournal(s.domain.StateRoot, s.domain.ID, string(name))
		if err != nil {
			return Record{}, err
		}
		record, startErr := starter.StartRebuildCandidate(ctx, string(name), j)
		if startErr != nil {
			return Record{}, startErr
		}
		if record.IntendedState != StateRunning || record.Readiness.Status != ReadinessReady {
			return Record{}, fmt.Errorf("candidate remains %s; retry exact rebuild after readiness converges", record.IntendedState)
		}
		if _, err := s.ConfirmReady(ctx, string(name), starter); err != nil {
			return Record{}, err
		}
	}
	return s.RetireOld(ctx, string(name), starter)
}
