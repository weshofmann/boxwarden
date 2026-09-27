package workspacex

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/weshofmann/boxwarden/internal/backend"
	"github.com/weshofmann/boxwarden/internal/domain"
	"github.com/weshofmann/boxwarden/internal/session"
)

func TestReleaseSessionUsesClearsBatchWhileStopping(t *testing.T) {
	root, stopped := stoppedLaunchFixture(t)
	addSecondQualifiedVolume(t, root)
	observer := stoppedObserver{state: backend.ObjectStopped, object: stopped.Backend.ObjectID}
	starting, err := PrepareSessionStart(context.Background(), root, domain.ID("work"), stopped, testGeneration, observer)
	if err != nil {
		t.Fatal(err)
	}
	stopping := starting
	stopping.IntendedState = session.StateStopping
	stopping.Readiness = session.ReadinessRecord{Status: session.ReadinessNotReady}
	if err := session.SaveRecord(root, domain.ID("work"), stopping); err != nil {
		t.Fatal(err)
	}
	if err := ReleaseSessionUses(context.Background(), root, domain.ID("work"), stopping, observer); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{testVolumeID, secondVolumeID} {
		volume, err := LoadRecord(root, domain.ID("work"), id)
		if err != nil || volume.Use != nil || volume.Attachment == nil {
			t.Fatalf("workspace %s after release: %#v, %v", id, volume, err)
		}
	}
	current, err := session.LoadRecord(root, "work", "dev")
	if err != nil || current != stopping {
		t.Fatalf("release changed session intent: %#v, %v", current, err)
	}
	if err := ReleaseSessionUses(context.Background(), root, domain.ID("work"), stopping, observer); err != nil {
		t.Fatalf("exact release retry failed: %v", err)
	}
}

func TestReleaseSessionUsesRetriesInterruptedSecondWrite(t *testing.T) {
	root, stopped := stoppedLaunchFixture(t)
	addSecondQualifiedVolume(t, root)
	observer := stoppedObserver{state: backend.ObjectStopped, object: stopped.Backend.ObjectID}
	starting, err := PrepareSessionStart(context.Background(), root, domain.ID("work"), stopped, testGeneration, observer)
	if err != nil {
		t.Fatal(err)
	}
	stopping := starting
	stopping.IntendedState = session.StateStopping
	stopping.Readiness = session.ReadinessRecord{Status: session.ReadinessNotReady}
	if err := session.SaveRecord(root, domain.ID("work"), stopping); err != nil {
		t.Fatal(err)
	}
	injected := errors.New("second release write failed")
	writes := 0
	err = releaseSessionUses(context.Background(), root, domain.ID("work"), stopping, observer, func(record Record) error {
		writes++
		if writes == 2 {
			return injected
		}
		return saveRecordTransition(root, domain.ID("work"), record, mutationReleaseUse, nil)
	})
	if !errors.Is(err, injected) || writes != 2 {
		t.Fatalf("interrupted release = %v, writes=%d", err, writes)
	}
	current, err := session.LoadRecord(root, "work", "dev")
	if err != nil || current != stopping {
		t.Fatalf("interrupted release advanced session: %#v, %v", current, err)
	}
	first, err := LoadRecord(root, domain.ID("work"), testVolumeID)
	if err != nil || first.Use != nil {
		t.Fatalf("first release not durable: %#v, %v", first.Use, err)
	}
	second, err := LoadRecord(root, domain.ID("work"), secondVolumeID)
	if err != nil || second.Use == nil {
		t.Fatalf("second Use not retained: %#v, %v", second.Use, err)
	}
	if err := ReleaseSessionUses(context.Background(), root, domain.ID("work"), stopping, observer); err != nil {
		t.Fatalf("exact batch retry failed: %v", err)
	}
}

func TestReleaseSessionUsesRejectsRunningBackendAndForeignUse(t *testing.T) {
	root, stopped := stoppedLaunchFixture(t)
	observer := stoppedObserver{state: backend.ObjectStopped, object: stopped.Backend.ObjectID}
	starting, err := PrepareSessionStart(context.Background(), root, domain.ID("work"), stopped, testGeneration, observer)
	if err != nil {
		t.Fatal(err)
	}
	stopping := starting
	stopping.IntendedState = session.StateStopping
	stopping.Readiness = session.ReadinessRecord{Status: session.ReadinessNotReady}
	if err := session.SaveRecord(root, domain.ID("work"), stopping); err != nil {
		t.Fatal(err)
	}
	if err := ReleaseSessionUses(context.Background(), root, domain.ID("work"), stopping, stoppedObserver{state: backend.ObjectRunning, object: stopped.Backend.ObjectID}); err == nil {
		t.Fatal("running backend released workspace Use")
	}
	volume, err := LoadRecord(root, domain.ID("work"), testVolumeID)
	if err != nil || volume.Use == nil {
		t.Fatalf("failed release lost Use: %#v, %v", volume.Use, err)
	}
	foreign := *volume.Use
	foreign.Generation = "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"
	volume.Use = &foreign
	// Simulate an already-present foreign reservation in the private record.
	if err := os.WriteFile(filepath.Join(root, "workspaces", testVolumeID+".json"), []byte(mustJSON(t, volume)), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ReleaseSessionUses(context.Background(), root, domain.ID("work"), stopping, observer); err == nil {
		t.Fatal("foreign generation released under stopping marker")
	}
}

func TestReleaseSessionUsesReconcilesStoppedPartialStart(t *testing.T) {
	root, stopped := stoppedLaunchFixture(t)
	addSecondQualifiedVolume(t, root)
	observer := stoppedObserver{state: backend.ObjectStopped, object: stopped.Backend.ObjectID}
	writes := 0
	_, err := prepareSessionStart(context.Background(), root, domain.ID("work"), stopped, testGeneration, observer, func(record Record) error {
		writes++
		if writes == 2 {
			return errors.New("injected second write failure")
		}
		return saveRecordTransition(root, domain.ID("work"), record, mutationReserveUse, nil)
	})
	if err == nil || writes != 2 {
		t.Fatalf("failed to construct partial batch: %v, writes=%d", err, writes)
	}
	if err := ReleaseSessionUses(context.Background(), root, domain.ID("work"), stopped, observer); err != nil {
		t.Fatalf("reconcile stopped partial batch: %v", err)
	}
	if _, err := PrepareSessionStart(context.Background(), root, domain.ID("work"), stopped, testGeneration, observer); err != nil {
		t.Fatalf("start after exact stopped reconciliation: %v", err)
	}
}
