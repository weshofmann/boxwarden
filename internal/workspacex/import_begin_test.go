package workspacex

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/weshofmann/boxwarden/internal/domain"
	"github.com/weshofmann/boxwarden/internal/session"
	"github.com/weshofmann/boxwarden/internal/supervisor"
)

type importReadyReader struct{ snapshot supervisor.Snapshot }

func (r importReadyReader) Snapshot(context.Context, supervisor.Binding) (supervisor.Snapshot, error) {
	return r.snapshot, nil
}

func readyImportFixture(t *testing.T) (string, importReadyReader) {
	t.Helper()
	root, started := launchFixture(t, true)
	started.IntendedState = session.StateRunning
	started.Readiness = session.ReadinessRecord{Status: session.ReadinessReady}
	if err := session.SaveRecord(root, domain.ID("work"), started); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "imports"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeCapturedImportFixture(t, root, testImportID)
	binding := supervisor.Binding{Domain: "work", SessionID: started.ID, BackendKind: "tart", BackendObject: started.Backend.ObjectID, Generation: started.StartGeneration}
	return root, importReadyReader{snapshot: supervisor.Snapshot{Binding: binding, ObservedAt: time.Now(), BackendRunning: true,
		SerialHealthy: true, PinPresent: true, CertificateCurrent: true, ProbeOK: true, ZoneMatches: true}}
}

func TestBeginImportRequiresExactLiveSessionAndVolume(t *testing.T) {
	root, reader := readyImportFixture(t)
	wrong := reader
	wrong.snapshot.Binding.Generation = "00000000-0000-4000-8000-000000000000"
	if _, err := BeginImport(context.Background(), root, domain.ID("work"), "dev", testVolumeID, testImportID, wrong); err == nil {
		t.Fatal("wrong live generation was admitted")
	}
	wrong = reader
	wrong.snapshot.ProbeOK = false
	if _, err := BeginImport(context.Background(), root, domain.ID("work"), "dev", testVolumeID, testImportID, wrong); err == nil {
		t.Fatal("unmounted or unprobed workspace was admitted")
	}
	journal, err := BeginImport(context.Background(), root, domain.ID("work"), "dev", testVolumeID, testImportID, reader)
	if err != nil || journal.Phase != ImportCaptured || journal.SessionID != reader.snapshot.Binding.SessionID || journal.Generation != reader.snapshot.Binding.Generation || journal.MountPath != "/home/boxwarden/workspaces/project" {
		t.Fatalf("begin exact import = %#v, %v", journal, err)
	}
	stored, err := loadImportJournal(root, domain.ID("work"), testImportID)
	if err != nil || stored != journal {
		t.Fatalf("journal binding = %#v, %v", stored, err)
	}
	if _, err := BeginImport(context.Background(), root, domain.ID("work"), "dev", testVolumeID, testImportID, reader); err != nil {
		t.Fatalf("exact begin retry failed: %v", err)
	}
}

func TestBeginImportRejectsChangedUseAndStaleReadiness(t *testing.T) {
	root, reader := readyImportFixture(t)
	reader.snapshot.ObservedAt = time.Now().Add(-time.Minute)
	if _, err := BeginImport(context.Background(), root, domain.ID("work"), "dev", testVolumeID, testImportID, reader); err == nil {
		t.Fatal("stale readiness was admitted")
	}
	reader.snapshot.ObservedAt = time.Now()
	volume, err := LoadRecord(root, domain.ID("work"), testVolumeID)
	if err != nil {
		t.Fatal(err)
	}
	volume.Use.Generation = "00000000-0000-4000-8000-000000000000"
	raw, err := json.Marshal(volume)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "workspaces", testVolumeID+".json"), append(raw, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := BeginImport(context.Background(), root, domain.ID("work"), "dev", testVolumeID, testImportID, reader); err == nil {
		t.Fatal("changed workspace use was admitted")
	}
}
