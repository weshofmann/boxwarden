package workspacex

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/weshofmann/boxwarden/internal/backend"
	"github.com/weshofmann/boxwarden/internal/domain"
	"github.com/weshofmann/boxwarden/internal/session"
)

const testImportExportID = "22222222-3333-4444-8555-666666666666"

func stoppedImportVerificationFixture(t *testing.T) (string, ImportJournal, ExportJournal, string, stoppedObserver) {
	t.Helper()
	root, reader := readyImportFixture(t)
	imported, err := BeginImport(t.Context(), root, domain.ID("work"), "dev", testVolumeID, testImportID, reader)
	if err != nil {
		t.Fatal(err)
	}
	transferring := imported
	transferring.Phase = ImportTransferring
	if err := advanceImportJournal(t.Context(), root, imported, transferring); err != nil {
		t.Fatal(err)
	}
	current, err := session.LoadRecord(root, "work", "dev")
	if err != nil {
		t.Fatal(err)
	}
	current.IntendedState = session.StateStopped
	current.StartGeneration = ""
	current.Readiness = session.ReadinessRecord{Status: session.ReadinessNotReady}
	if err := session.SaveRecord(root, domain.ID("work"), current); err != nil {
		t.Fatal(err)
	}
	observer := stoppedObserver{state: backend.ObjectStopped, object: current.Backend.ObjectID}
	if err := ReleaseSessionUses(t.Context(), root, domain.ID("work"), current, observer); err != nil {
		t.Fatal(err)
	}
	volume, err := LoadRecord(root, domain.ID("work"), testVolumeID)
	if err != nil {
		t.Fatal(err)
	}
	destination := privateRoot(t)
	parentInfo, err := os.Lstat(destination)
	if err != nil {
		t.Fatal(err)
	}
	destinationIdentity, err := diskIdentity(parentInfo)
	if err != nil {
		t.Fatal(err)
	}
	exported := ExportJournal{Version: 1, ID: testImportExportID, Domain: imported.Domain, VolumeID: imported.VolumeID,
		SessionID: imported.SessionID, SessionName: imported.SessionName, BackendObject: imported.BackendObject,
		FilesystemUUID: imported.FilesystemUUID, SizeBytes: volume.SizeBytes, Source: *volume.Disk,
		SnapshotPath: filepath.Join("exports", testImportExportID, "snapshot.raw"), DestinationParent: destination,
		Destination: destinationIdentity, Selected: []string{"boxwarden-import-" + imported.ID}, Phase: ExportCopying}
	if err := createExportJournal(root, exported); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "exports", exported.ID), 0o700); err != nil {
		t.Fatal(err)
	}
	source, err := os.Open(filepath.Join(root, "volumes", testVolumeID+".raw"))
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	copyPath := filepath.Join(root, exported.SnapshotPath)
	copyFile, err := os.OpenFile(copyPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	hasher := sha256.New()
	if _, err := io.Copy(io.MultiWriter(copyFile, hasher), source); err != nil {
		t.Fatal(err)
	}
	if err := copyFile.Sync(); err != nil {
		t.Fatal(err)
	}
	if err := copyFile.Close(); err != nil {
		t.Fatal(err)
	}
	copyInfo, err := os.Lstat(copyPath)
	if err != nil {
		t.Fatal(err)
	}
	copyIdentity, err := diskIdentity(copyInfo)
	if err != nil {
		t.Fatal(err)
	}
	ready := exported
	ready.Phase = ExportSnapshotReady
	ready.Snapshot = &ExportSnapshot{Identity: copyIdentity, SHA256: hex.EncodeToString(hasher.Sum(nil))}
	if err := advanceExportJournal(t.Context(), root, exported, ready); err != nil {
		t.Fatal(err)
	}
	inspected := ready
	inspected.Phase = ExportInspected
	if err := advanceExportJournal(t.Context(), root, ready, inspected); err != nil {
		t.Fatal(err)
	}
	exported = inspected
	exported.Phase = ExportPublished
	if err := advanceExportJournal(t.Context(), root, inspected, exported); err != nil {
		t.Fatal(err)
	}
	transaction := filepath.Join(destination, exportTransactionHex(exported.ID))
	if err := os.Mkdir(transaction, 0o700); err != nil {
		t.Fatal(err)
	}
	selected := filepath.Join(transaction, exported.Selected[0])
	if err := os.Mkdir(selected, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(selected, "project.txt"), []byte("synthetic\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return root, transferring, exported, selected, observer
}

func TestVerifyStoppedImportBindsExactPublishedDiskAndIsIdempotent(t *testing.T) {
	root, imported, exported, _, observer := stoppedImportVerificationFixture(t)
	verified, err := VerifyStoppedImport(t.Context(), root, imported.Domain, imported.ID, exported.ID, observer)
	if err != nil || verified.Phase != ImportVerified || verified.ExportID != exported.ID {
		t.Fatalf("exact stopped import not verified: %+v, %v", verified, err)
	}
	again, err := VerifyStoppedImport(t.Context(), root, imported.Domain, imported.ID, exported.ID, observer)
	if err != nil || again != verified {
		t.Fatalf("exact verified retry changed result: %+v, %v", again, err)
	}
	if _, err := VerifyStoppedImport(t.Context(), root, imported.Domain, imported.ID, testGeneration, observer); err == nil {
		t.Fatal("different export adopted after verification")
	}
}

func TestVerifyStoppedImportRejectsFalseOrUnstableEvidence(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*testing.T, string, ImportJournal, ExportJournal, string, *stoppedObserver)
	}{
		{"changed published bytes", func(t *testing.T, _ string, _ ImportJournal, _ ExportJournal, selected string, _ *stoppedObserver) {
			if err := os.WriteFile(filepath.Join(selected, "project.txt"), []byte("altered!!\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{"wrong selection", func(t *testing.T, root string, _ ImportJournal, exported ExportJournal, _ string, _ *stoppedObserver) {
			exported.Selected = []string{"boxwarden-import-" + testImportID + "/project.txt"}
			raw, err := json.Marshal(exported)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, "exports", exported.ID+".json"), append(raw, '\n'), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{"running backend", func(_ *testing.T, _ string, _ ImportJournal, _ ExportJournal, _ string, observer *stoppedObserver) {
			observer.state = backend.ObjectRunning
		}},
		{"changed disk identity", func(t *testing.T, root string, imported ImportJournal, _ ExportJournal, _ string, _ *stoppedObserver) {
			volume, err := LoadRecord(root, imported.Domain, imported.VolumeID)
			if err != nil {
				t.Fatal(err)
			}
			volume.Disk.Inode++
			raw, err := json.Marshal(volume)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, "workspaces", imported.VolumeID+".json"), append(raw, '\n'), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			root, imported, exported, selected, observer := stoppedImportVerificationFixture(t)
			test.change(t, root, imported, exported, selected, &observer)
			if _, err := VerifyStoppedImport(context.Background(), root, imported.Domain, imported.ID, exported.ID, observer); err == nil {
				t.Fatal("false stopped import evidence advanced verified phase")
			}
			stored, err := LoadImportJournal(root, imported.Domain, imported.ID)
			if err != nil || stored != imported {
				t.Fatalf("failed verification changed import journal: %+v, %v", stored, err)
			}
		})
	}
}
