package session

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/weshofmann/boxwarden/internal/backend"
	"github.com/weshofmann/boxwarden/internal/backend/fake"
	"github.com/weshofmann/boxwarden/internal/domain"
	"github.com/weshofmann/boxwarden/internal/golden"
	"github.com/weshofmann/boxwarden/internal/sshx"
	"github.com/weshofmann/boxwarden/internal/supervisor"
	"time"
)

type absentRebuildPin struct{}

func (absentRebuildPin) Load(context.Context, sshx.Binding) (sshx.HostKeyPin, error) {
	return sshx.HostKeyPin{}, os.ErrNotExist
}

func rebuildPreparationFixture(t *testing.T) (*RebuildService, *fake.Backend, Record) {
	t.Helper()
	configured, backendFake, creator := createFixture(t)
	old, err := creator.Create(context.Background(), "dev", ModeClean)
	if err != nil {
		t.Fatal(err)
	}
	backendFake.SetObservation(backend.Observation{ObjectID: "golden-r2", Exists: true, State: backend.ObjectStopped})
	if _, err := golden.Register(context.Background(), configured, "golden-r2", backendFake); err != nil {
		t.Fatal(err)
	}
	service := NewRebuildService(configured, RebuildDependencies{
		Observer: backendFake, Creator: backendFake, Deleter: backendFake, Pins: absentRebuildPin{},
		Gate: func(_ context.Context, root string, domainID domain.ID, expected Record, _ backend.Observer, reserve func() error) error {
			if root != configured.StateRoot || domainID != configured.ID || expected != old {
				t.Fatal("rebuild gate received wrong old system")
			}
			return reserve()
		},
	})
	service.newID = func() (string, error) { return "7fb25db7-3cc1-4d92-a04c-b60fd05fa421", nil }
	return service, backendFake, old
}

func TestPrepareRebuildCandidatePersistsIntentBeforeCloneAndKeepsOldSystem(t *testing.T) {
	service, backendFake, old := rebuildPreparationFixture(t)
	backendFake.SetCloneFault(func(_ context.Context, call fake.CloneCall) error {
		if call.SourceID != "golden-r2" {
			t.Fatalf("rebuild cloned from %q", call.SourceID)
		}
		journal, err := LoadRebuildJournal(service.domain.StateRoot, old.Domain, string(old.Name))
		if err != nil || journal.Phase != RebuildReserved || journal.CandidateBackend != call.TargetID {
			t.Fatalf("clone lacked durable exact reservation: %#v, %v", journal, err)
		}
		return nil
	})
	journal, err := service.PrepareCandidate(context.Background(), "dev", "golden-r2")
	if err != nil || journal.Phase != RebuildCloned || journal.OldBackend != old.Backend.ObjectID || journal.CandidateRevision != "golden-r2" {
		t.Fatalf("prepared rebuild candidate = %#v, %v", journal, err)
	}
	current, err := LoadRecord(service.domain.StateRoot, "work", "dev")
	if err != nil || current != old {
		t.Fatalf("candidate preparation changed active session: %#v, %v", current, err)
	}
	if len(backendFake.CloneCalls()) != 2 || len(backendFake.RandomizeMACCalls()) != 2 {
		t.Fatalf("clone/MAC calls = %d/%d, want initial and candidate", len(backendFake.CloneCalls()), len(backendFake.RandomizeMACCalls()))
	}
	again, err := service.PrepareCandidate(context.Background(), "dev", "golden-r2")
	if err != nil || again != journal || len(backendFake.CloneCalls()) != 2 || len(backendFake.RandomizeMACCalls()) != 2 {
		t.Fatalf("idempotent candidate retry = %#v, %v; clone/MAC %d/%d", again, err, len(backendFake.CloneCalls()), len(backendFake.RandomizeMACCalls()))
	}
}

func TestPrepareRebuildCandidateReconcilesClonePostEffectFailure(t *testing.T) {
	service, backendFake, old := rebuildPreparationFixture(t)
	injected := errors.New("clone returned after backend effect")
	backendFake.SetClonePostEffectError(injected)
	if _, err := service.PrepareCandidate(context.Background(), "dev", "golden-r2"); !errors.Is(err, injected) {
		t.Fatalf("clone error = %v, want post-effect fault", err)
	}
	journal, err := LoadRebuildJournal(service.domain.StateRoot, old.Domain, string(old.Name))
	if err != nil || journal.Phase != RebuildReserved || len(backendFake.CloneCalls()) != 2 {
		t.Fatalf("reserved uncertain clone = %#v, %v; calls=%d", journal, err, len(backendFake.CloneCalls()))
	}
	backendFake.SetClonePostEffectError(nil)
	prepared, err := service.PrepareCandidate(context.Background(), "dev", "golden-r2")
	if err != nil || prepared.Phase != RebuildCloned || len(backendFake.CloneCalls()) != 2 {
		t.Fatalf("reconciled exact stopped clone = %#v, %v; calls=%d", prepared, err, len(backendFake.CloneCalls()))
	}
}

func TestPrepareRebuildCandidateRejectsRunningRecordedCandidate(t *testing.T) {
	service, backendFake, old := rebuildPreparationFixture(t)
	backendFake.SetClonePostEffectError(errors.New("ambiguous clone reply"))
	if _, err := service.PrepareCandidate(context.Background(), "dev", "golden-r2"); err == nil {
		t.Fatal("expected clone reply failure")
	}
	journal, err := LoadRebuildJournal(service.domain.StateRoot, old.Domain, string(old.Name))
	if err != nil {
		t.Fatal(err)
	}
	backendFake.SetClonePostEffectError(nil)
	backendFake.SetObservation(backend.Observation{ObjectID: journal.CandidateBackend, Exists: true, State: backend.ObjectRunning})
	if _, err := service.PrepareCandidate(context.Background(), "dev", "golden-r2"); err == nil {
		t.Fatal("running candidate was adopted as a stopped never-booted clone")
	}
	if len(backendFake.CloneCalls()) != 2 || len(backendFake.RandomizeMACCalls()) != 1 {
		t.Fatalf("ambiguous candidate was mutated: clone/MAC %d/%d", len(backendFake.CloneCalls()), len(backendFake.RandomizeMACCalls()))
	}
}

func TestRebuildCutoverPersistsJournalBeforeStableSessionSwitch(t *testing.T) {
	service, _, old := rebuildPreparationFixture(t)
	journal, err := service.PrepareCandidate(context.Background(), "dev", "golden-r2")
	if err != nil {
		t.Fatal(err)
	}
	injected := errors.New("stop after durable cutover intent")
	service.cutoverHook = func() error {
		current, err := LoadRebuildJournal(service.domain.StateRoot, old.Domain, "dev")
		if err != nil || current.Phase != RebuildCutover || current.CandidateBackend != journal.CandidateBackend {
			t.Fatalf("cutover journal absent before session switch: %#v, %v", current, err)
		}
		stored, err := LoadRecord(service.domain.StateRoot, "work", "dev")
		if err != nil || stored != old {
			t.Fatalf("old session changed before cutover intent: %#v, %v", stored, err)
		}
		return injected
	}
	if _, err := service.Cutover(context.Background(), "dev"); !errors.Is(err, injected) {
		t.Fatalf("injected cutover interruption = %v", err)
	}
	service.cutoverHook = nil
	active, err := service.Cutover(context.Background(), "dev")
	if err != nil || active.ID != old.ID || active.Name != old.Name || active.Backend.ObjectID != journal.CandidateBackend ||
		active.GoldenRevision != "golden-r2" || active.IntendedState != StateStopped {
		t.Fatalf("reconciled active candidate = %#v, %v", active, err)
	}
	if again, err := service.Cutover(context.Background(), "dev"); err != nil || again != active {
		t.Fatalf("idempotent cutover = %#v, %v", again, err)
	}
}

func TestRebuildCutoverRejectsRunningOldOrCandidate(t *testing.T) {
	for _, running := range []string{"old", "candidate"} {
		t.Run(running, func(t *testing.T) {
			service, backendFake, old := rebuildPreparationFixture(t)
			journal, err := service.PrepareCandidate(context.Background(), "dev", "golden-r2")
			if err != nil {
				t.Fatal(err)
			}
			object := old.Backend.ObjectID
			if running == "candidate" {
				object = journal.CandidateBackend
			}
			backendFake.SetObservation(backend.Observation{ObjectID: object, Exists: true, State: backend.ObjectRunning})
			if _, err := service.Cutover(context.Background(), "dev"); err == nil {
				t.Fatal("running system entered cutover")
			}
			current, err := LoadRecord(service.domain.StateRoot, "work", "dev")
			if err != nil || current != old {
				t.Fatalf("rejected cutover changed active system: %#v, %v", current, err)
			}
			currentJournal, err := LoadRebuildJournal(service.domain.StateRoot, old.Domain, "dev")
			if err != nil || currentJournal.Phase != RebuildCloned {
				t.Fatalf("rejected cutover changed journal: %#v, %v", currentJournal, err)
			}
		})
	}
}

func TestStartRebuildCandidateUsesExactJournalAndOrdinaryStartStaysBlocked(t *testing.T) {
	rebuilder, backendFake, old := rebuildPreparationFixture(t)
	prepared, err := rebuilder.PrepareCandidate(context.Background(), "dev", "golden-r2")
	if err != nil {
		t.Fatal(err)
	}
	active, err := rebuilder.Cutover(context.Background(), "dev")
	if err != nil {
		t.Fatal(err)
	}
	journal, err := LoadRebuildJournal(rebuilder.domain.StateRoot, old.Domain, "dev")
	if err != nil || journal.Phase != RebuildCutover || journal.CandidateBackend != prepared.CandidateBackend {
		t.Fatalf("cutover journal = %#v, %v", journal, err)
	}
	control := &startSupervisorFake{start: func(request supervisor.LaunchRequest) (supervisor.Snapshot, error) {
		if request.Binding.BackendObject != journal.CandidateBackend || request.Binding.SessionID != old.ID {
			t.Fatalf("supervisor received wrong rebuild binding: %#v", request.Binding)
		}
		return readySnapshot(request.Binding, time.Now()), nil
	}}
	starter := newStartTestService(rebuilder.domain, backendFake, control, time.Now, func() (string, error) { return testStartGeneration, nil })
	preparedWorkspaces := 0
	starter.start.Workspaces = startWorkspaceFake{rebuildPrepare: func(got RebuildJournal) error {
		preparedWorkspaces++
		if got != journal {
			t.Fatalf("workspace coordinator received foreign journal: %#v", got)
		}
		return nil
	}}
	if _, err := starter.Start(context.Background(), "dev"); err == nil || control.startCalls != 0 || preparedWorkspaces != 0 {
		t.Fatalf("ordinary start bypassed journal: error=%v launch=%d workspaces=%d", err, control.startCalls, preparedWorkspaces)
	}
	wrong := journal
	wrong.OldRevision = "foreign-old"
	if _, err := starter.StartRebuildCandidate(context.Background(), "dev", wrong); err == nil || control.startCalls != 0 {
		t.Fatal("wrong journal authorized candidate start")
	}
	started, err := starter.StartRebuildCandidate(context.Background(), "dev", journal)
	if err != nil || started.IntendedState != StateRunning || started.Backend.ObjectID != journal.CandidateBackend ||
		started.ID != active.ID || started.StartGeneration != testStartGeneration || preparedWorkspaces != 1 || control.startCalls != 1 {
		t.Fatalf("journaled candidate start = %#v, %v; workspaces=%d launch=%d", started, err, preparedWorkspaces, control.startCalls)
	}
	control.snapshot = func(binding supervisor.Binding) (supervisor.Snapshot, error) {
		incomplete := readySnapshot(binding, time.Now())
		incomplete.ZoneMatches = false
		return incomplete, nil
	}
	if _, err := rebuilder.ConfirmReady(context.Background(), "dev", starter); err == nil {
		t.Fatal("incomplete fresh owner evidence advanced rebuild ready")
	}
	currentJournal, err := LoadRebuildJournal(rebuilder.domain.StateRoot, old.Domain, "dev")
	if err != nil || currentJournal.Phase != RebuildCutover {
		t.Fatalf("incomplete readiness changed journal: %#v, %v", currentJournal, err)
	}
	control.snapshot = func(binding supervisor.Binding) (supervisor.Snapshot, error) {
		return readySnapshot(binding, time.Now()), nil
	}
	rebuilder.readyHook = func() error { return errors.New("interrupt after fresh ready evidence") }
	if _, err := rebuilder.ConfirmReady(context.Background(), "dev", starter); err == nil {
		t.Fatal("expected readiness journal interruption")
	}
	currentJournal, err = LoadRebuildJournal(rebuilder.domain.StateRoot, old.Domain, "dev")
	if err != nil || currentJournal.Phase != RebuildCutover {
		t.Fatalf("interrupted ready advancement changed journal: %#v, %v", currentJournal, err)
	}
	rebuilder.readyHook = nil
	confirmed, err := rebuilder.ConfirmReady(context.Background(), "dev", starter)
	if err != nil || confirmed.IntendedState != StateRunning || confirmed.Readiness.Status != ReadinessReady {
		t.Fatalf("fresh candidate READY confirmation = %#v, %v", confirmed, err)
	}
	currentJournal, err = LoadRebuildJournal(rebuilder.domain.StateRoot, old.Domain, "dev")
	if err != nil || currentJournal.Phase != RebuildReady {
		t.Fatalf("candidate ready phase = %#v, %v", currentJournal, err)
	}
	if retried, err := rebuilder.ConfirmReady(context.Background(), "dev", starter); err != nil || retried != confirmed {
		t.Fatalf("ready retry = %#v, %v", retried, err)
	}
	backendFake.SetObservation(backend.Observation{ObjectID: old.Backend.ObjectID, State: backend.ObjectUnknown})
	if _, err := rebuilder.RetireOld(context.Background(), "dev", starter); err == nil || len(backendFake.DeleteCalls()) != 0 {
		t.Fatalf("missing old object before deletion intent was accepted: %v; calls=%v", err, backendFake.DeleteCalls())
	}
	backendFake.SetObservation(backend.Observation{ObjectID: old.Backend.ObjectID, Exists: true, State: backend.ObjectStopped})
	retired, err := rebuilder.RetireOld(context.Background(), "dev", starter)
	if err != nil || retired != confirmed || len(backendFake.DeleteCalls()) != 1 || backendFake.DeleteCalls()[0] != old.Backend.ObjectID {
		t.Fatalf("old-system retirement = %#v, %v; deleted=%v", retired, err, backendFake.DeleteCalls())
	}
	if _, err := LoadRebuildJournal(rebuilder.domain.StateRoot, old.Domain, "dev"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("completed rebuild journal remained: %v", err)
	}
}

type postEffectRebuildDeleter struct {
	inner backend.Deleter
	err   error
}

func (d postEffectRebuildDeleter) Delete(ctx context.Context, objectID string) error {
	if err := d.inner.Delete(ctx, objectID); err != nil {
		return err
	}
	return d.err
}

func TestRebuildRetirementReconcilesPostEffectDeleteFailure(t *testing.T) {
	rebuilder, backendFake, old := rebuildPreparationFixture(t)
	if _, err := rebuilder.PrepareCandidate(context.Background(), "dev", "golden-r2"); err != nil {
		t.Fatal(err)
	}
	if _, err := rebuilder.Cutover(context.Background(), "dev"); err != nil {
		t.Fatal(err)
	}
	control := &startSupervisorFake{
		start: func(r supervisor.LaunchRequest) (supervisor.Snapshot, error) {
			return readySnapshot(r.Binding, time.Now()), nil
		},
		snapshot: func(b supervisor.Binding) (supervisor.Snapshot, error) { return readySnapshot(b, time.Now()), nil },
	}
	starter := newStartTestService(rebuilder.domain, backendFake, control, time.Now, func() (string, error) { return testStartGeneration, nil })
	if _, err := starter.StartRebuildCandidate(context.Background(), "dev", mustRebuildJournal(t, rebuilder.domain.StateRoot, old.Domain)); err != nil {
		t.Fatal(err)
	}
	if _, err := rebuilder.ConfirmReady(context.Background(), "dev", starter); err != nil {
		t.Fatal(err)
	}
	injected := errors.New("delete completed but reply failed")
	rebuilder.deps.Deleter = postEffectRebuildDeleter{inner: backendFake, err: injected}
	if _, err := rebuilder.RetireOld(context.Background(), "dev", starter); !errors.Is(err, injected) {
		t.Fatalf("post-effect delete error = %v", err)
	}
	j := mustRebuildJournal(t, rebuilder.domain.StateRoot, old.Domain)
	if j.Phase != RebuildRetiring || len(backendFake.DeleteCalls()) != 1 {
		t.Fatalf("uncertain delete lost retirement intent: %#v calls=%v", j, backendFake.DeleteCalls())
	}
	rebuilder.deps.Deleter = backendFake
	if _, err := rebuilder.RetireOld(context.Background(), "dev", starter); err != nil || len(backendFake.DeleteCalls()) != 1 {
		t.Fatalf("retirement retry recloned or re-deleted: %v calls=%v", err, backendFake.DeleteCalls())
	}
	if _, err := LoadRebuildJournal(rebuilder.domain.StateRoot, old.Domain, "dev"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("retirement retry left journal: %v", err)
	}
}

func mustRebuildJournal(t *testing.T, root string, domainID domain.ID) RebuildJournal {
	t.Helper()
	j, err := LoadRebuildJournal(root, domainID, "dev")
	if err != nil {
		t.Fatal(err)
	}
	return j
}

func TestExecuteRebuildRunsAllPhasesAndRepeatedBaseDoesNotReclone(t *testing.T) {
	rebuilder, backendFake, old := rebuildPreparationFixture(t)
	control := &startSupervisorFake{
		start: func(r supervisor.LaunchRequest) (supervisor.Snapshot, error) {
			return readySnapshot(r.Binding, time.Now()), nil
		},
		snapshot: func(b supervisor.Binding) (supervisor.Snapshot, error) { return readySnapshot(b, time.Now()), nil },
	}
	starter := newStartTestService(rebuilder.domain, backendFake, control, time.Now, func() (string, error) { return testStartGeneration, nil })
	result, err := rebuilder.Execute(context.Background(), "dev", "golden-r2", starter)
	if err != nil || result.ID != old.ID || result.GoldenRevision != "golden-r2" || result.IntendedState != StateRunning {
		t.Fatalf("complete rebuild = %#v, %v", result, err)
	}
	if _, err := LoadRebuildJournal(rebuilder.domain.StateRoot, old.Domain, "dev"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("completed rebuild retained journal: %v", err)
	}
	if again, err := rebuilder.Execute(context.Background(), "dev", "golden-r2", starter); err != nil || again != result ||
		len(backendFake.CloneCalls()) != 2 || len(backendFake.DeleteCalls()) != 1 || control.startCalls != 1 {
		t.Fatalf("repeated completed rebuild = %#v, %v; clone=%d delete=%d start=%d", again, err, len(backendFake.CloneCalls()), len(backendFake.DeleteCalls()), control.startCalls)
	}
}
