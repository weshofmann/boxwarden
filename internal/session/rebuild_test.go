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
		Observer: backendFake, Creator: backendFake, Pins: absentRebuildPin{},
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
