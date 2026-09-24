package workspacex

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/weshofmann/boxwarden/internal/backend"
	"github.com/weshofmann/boxwarden/internal/domain"
	"github.com/weshofmann/boxwarden/internal/session"
	"github.com/weshofmann/boxwarden/internal/workspaceformat"
)

func TestPrepareRebuildSessionStartReservesOnlyExactCandidateUses(t *testing.T) {
	root, stopped := stoppedLaunchFixture(t)
	journal := session.RebuildJournal{Version: 1, Domain: domain.ID("work"), SessionName: "dev", SessionID: stopped.ID,
		OperationID: "7fb25db7-3cc1-4d92-a04c-b60fd05fa421", Phase: session.RebuildCutover,
		OldBackend: stopped.Backend.ObjectID, OldRevision: stopped.GoldenRevision,
		CandidateBackend: "boxwarden-work-7fb25db73cc14d92a04cb60fd05fa421", CandidateRevision: "golden-r2"}
	stopped.Backend.ObjectID = journal.CandidateBackend
	stopped.GoldenRevision = journal.CandidateRevision
	if err := session.SaveRecord(root, domain.ID("work"), stopped); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "rebuilds"), 0o700); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(journal)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "rebuilds", "dev.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	observer := stoppedObserver{state: backend.ObjectStopped, object: journal.CandidateBackend}
	if _, err := PrepareSessionStart(context.Background(), root, domain.ID("work"), stopped, testGeneration, observer); err == nil {
		t.Fatal("ordinary start bypassed pending rebuild journal")
	}
	wrong := journal
	wrong.CandidateRevision = "foreign-base"
	if _, err := PrepareRebuildSessionStart(context.Background(), root, domain.ID("work"), stopped, testGeneration, observer, wrong); err == nil {
		t.Fatal("foreign rebuild journal reserved a workspace Use")
	}
	started, err := PrepareRebuildSessionStart(context.Background(), root, domain.ID("work"), stopped, testGeneration, observer, journal)
	if err != nil || started.IntendedState != session.StateStarting || started.Backend.ObjectID != journal.CandidateBackend {
		t.Fatalf("rebuild start reservation = %#v, %v", started, err)
	}
	volume, err := LoadRecord(root, domain.ID("work"), testVolumeID)
	if err != nil || volume.Use == nil || *volume.Use != (Use{BackendKind: "tart", BackendObject: journal.CandidateBackend, Generation: testGeneration}) ||
		volume.Attachment == nil || volume.Attachment.SessionID != stopped.ID {
		t.Fatalf("candidate Use or stable attachment = %#v, %v", volume, err)
	}
}

const secondVolumeID = "00112233-4455-4677-8899-aabbccddff00"

func addSecondQualifiedVolume(t *testing.T, root string) {
	t.Helper()
	volume := fixtureRecord()
	volume.VolumeID = secondVolumeID
	volume.FilesystemUUID = "11111111-2222-4333-8444-555555555556"
	volume.SizeBytes = 16 << 20
	if err := SaveRecord(root, domain.ID("work"), volume); err != nil {
		t.Fatal(err)
	}
	request := workspaceformat.Request{Domain: volume.Domain, VolumeID: volume.VolumeID, FilesystemUUID: volume.FilesystemUUID, SizeBytes: volume.SizeBytes}
	if _, err := workspaceformat.Create(context.Background(), root, request, formatFixture(func(_ context.Context, request workspaceformat.FormatRequest) (workspaceformat.FormatEvidence, error) {
		if err := writeFixtureExt4Header(request.DiskPath, request.FilesystemUUID); err != nil {
			return workspaceformat.FormatEvidence{}, err
		}
		return workspaceformat.FormatEvidence{ObservedUUID: request.FilesystemUUID, WholeDevice: true, FilesystemClean: true}, nil
	})); err != nil {
		t.Fatal(err)
	}
	if _, err := PromoteVerified(context.Background(), root, domain.ID("work"), volume.VolumeID); err != nil {
		t.Fatal(err)
	}
	if _, err := Attach(context.Background(), root, domain.ID("work"), volume.VolumeID, "dev", "/home/boxwarden/workspaces/second", stoppedObserver{state: backend.ObjectStopped, object: "bw-work-dev"}); err != nil {
		t.Fatal(err)
	}
}

func stoppedLaunchFixture(t *testing.T) (string, session.Record) {
	t.Helper()
	root, record := launchFixture(t, false)
	record.IntendedState = session.StateStopped
	record.StartGeneration = ""
	record.Readiness = session.ReadinessRecord{Status: session.ReadinessNotReady}
	if err := session.SaveRecord(root, domain.ID("work"), record); err != nil {
		t.Fatal(err)
	}
	return root, record
}

func TestPrepareSessionStartPersistsUseBeforeStartingMarker(t *testing.T) {
	root, stopped := stoppedLaunchFixture(t)
	addSecondQualifiedVolume(t, root)
	started, err := PrepareSessionStart(context.Background(), root, domain.ID("work"), stopped, testGeneration, stoppedObserver{state: backend.ObjectStopped, object: stopped.Backend.ObjectID})
	if err != nil {
		t.Fatal(err)
	}
	if started.IntendedState != session.StateStarting || started.StartGeneration != testGeneration || started.Readiness.Status != session.ReadinessStarting {
		t.Fatalf("incorrect start marker: %#v", started)
	}
	volume, err := LoadRecord(root, domain.ID("work"), testVolumeID)
	if err != nil || volume.Use == nil || *volume.Use != (Use{BackendKind: "tart", BackendObject: stopped.Backend.ObjectID, Generation: testGeneration}) {
		t.Fatalf("exact use missing at starting marker: %#v, %v", volume.Use, err)
	}
	set, err := AdmitLaunchDisks(context.Background(), root, domain.ID("work"), started, stoppedObserver{state: backend.ObjectStopped, object: stopped.Backend.ObjectID})
	if err != nil {
		t.Fatalf("child rejected committed batch: %v", err)
	}
	if err := set.CloseUnclaimed(); err != nil {
		t.Fatal(err)
	}
	second, err := LoadRecord(root, domain.ID("work"), secondVolumeID)
	if err != nil || second.Use == nil || *second.Use != *volume.Use {
		t.Fatalf("second exact Use missing at batch marker: %#v, %v", second.Use, err)
	}
}

func TestPrepareSessionStartSecondWriteFailureRetainsStoppedMarker(t *testing.T) {
	root, stopped := stoppedLaunchFixture(t)
	addSecondQualifiedVolume(t, root)
	injected := errors.New("second workspace write failed")
	writes := 0
	_, err := prepareSessionStart(context.Background(), root, domain.ID("work"), stopped, testGeneration, stoppedObserver{state: backend.ObjectStopped, object: stopped.Backend.ObjectID}, func(record Record) error {
		writes++
		if writes == 2 {
			return injected
		}
		return saveRecordTransition(root, domain.ID("work"), record, mutationReserveUse, nil)
	})
	if !errors.Is(err, injected) || writes != 2 {
		t.Fatalf("interrupted batch = %v, writes=%d", err, writes)
	}
	current, err := session.LoadRecord(root, "work", "dev")
	if err != nil || current.IntendedState != session.StateStopped {
		t.Fatalf("partial batch published Starting: %#v, %v", current, err)
	}
	first, err := LoadRecord(root, domain.ID("work"), testVolumeID)
	if err != nil || first.Use == nil {
		t.Fatalf("first durable Use missing: %#v, %v", first.Use, err)
	}
	second, err := LoadRecord(root, domain.ID("work"), secondVolumeID)
	if err != nil || second.Use != nil {
		t.Fatalf("second Use unexpectedly present: %#v, %v", second.Use, err)
	}
	if _, err := PrepareSessionStart(context.Background(), root, domain.ID("work"), stopped, testGeneration, stoppedObserver{state: backend.ObjectStopped, object: stopped.Backend.ObjectID}); err == nil {
		t.Fatal("partial prior batch admitted another start")
	}
}

func TestPrepareSessionStartRejectsChangedSessionBeforeCommit(t *testing.T) {
	root, stopped := stoppedLaunchFixture(t)
	changed := stopped
	changed.GoldenRevision = "different-revision"
	if err := session.SaveRecord(root, domain.ID("work"), changed); err != nil {
		t.Fatal(err)
	}
	if _, err := PrepareSessionStart(context.Background(), root, domain.ID("work"), stopped, testGeneration, stoppedObserver{state: backend.ObjectStopped, object: stopped.Backend.ObjectID}); err == nil {
		t.Fatal("changed session reached start commit")
	}
	volume, err := LoadRecord(root, domain.ID("work"), testVolumeID)
	if err != nil || volume.Use != nil {
		t.Fatalf("changed session wrote a Use: %#v, %v", volume.Use, err)
	}
}

func TestPrepareSessionStartDoesNotWaitForVolumeWithSessionLock(t *testing.T) {
	root, stopped := stoppedLaunchFixture(t)
	held, err := AcquireVolumeUse(context.Background(), root, domain.ID("work"), testVolumeID)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Millisecond)
	defer cancel()
	if _, err := PrepareSessionStart(ctx, root, domain.ID("work"), stopped, testGeneration, stoppedObserver{state: backend.ObjectStopped, object: stopped.Backend.ObjectID}); err == nil {
		t.Fatal("reservation bypassed held volume lock")
	}
	if err := held.Release(); err != nil {
		t.Fatal(err)
	}
	current, err := session.LoadRecord(root, "work", "dev")
	if err != nil || current.IntendedState != session.StateStopped {
		t.Fatalf("blocked reservation changed session: %#v, %v", current, err)
	}
}

func TestVerifySessionUsesRejectsMissingStartingReservation(t *testing.T) {
	root, stopped := stoppedLaunchFixture(t)
	started, err := PrepareSessionStart(context.Background(), root, domain.ID("work"), stopped, testGeneration, stoppedObserver{state: backend.ObjectStopped, object: stopped.Backend.ObjectID})
	if err != nil {
		t.Fatal(err)
	}
	volume, err := LoadRecord(root, domain.ID("work"), testVolumeID)
	if err != nil {
		t.Fatal(err)
	}
	volume.Use = nil
	if err := saveRecordTransition(root, domain.ID("work"), volume, mutationReleaseUse, nil); err != nil {
		t.Fatal(err)
	}
	if err := VerifySessionUses(context.Background(), root, domain.ID("work"), started); err == nil {
		t.Fatal("starting retry accepted missing durable Use")
	}
}
