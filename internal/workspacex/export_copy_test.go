package workspacex

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/weshofmann/boxwarden/internal/backend"
	"github.com/weshofmann/boxwarden/internal/domain"
	"github.com/weshofmann/boxwarden/internal/workspaceformat"
)

func TestCreateExportSnapshotCopiesStoppedQualifiedVolumeBeforeReleasingPending(t *testing.T) {
	root, stopped := stoppedLaunchFixture(t)
	parent := t.TempDir()
	if err := os.Chmod(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	journal, err := CreateExportSnapshot(context.Background(), root, domain.ID("work"), testVolumeID, parent, []string{"project/report.txt"}, stoppedObserver{state: backend.ObjectStopped, object: stopped.Backend.ObjectID})
	if err != nil {
		t.Fatal(err)
	}
	if journal.Phase != ExportSnapshotReady || journal.Snapshot == nil {
		t.Fatalf("snapshot not journaled: %#v", journal)
	}
	volume, err := LoadRecord(root, domain.ID("work"), testVolumeID)
	if err != nil || volume.Pending != nil || volume.Use != nil {
		t.Fatalf("snapshot left volume unavailable: %#v, %v", volume, err)
	}
	source, err := os.ReadFile(filepath.Join(root, "volumes", testVolumeID+".raw"))
	if err != nil {
		t.Fatal(err)
	}
	copyBytes, err := os.ReadFile(filepath.Join(root, journal.SnapshotPath))
	if err != nil {
		t.Fatal(err)
	}
	if len(copyBytes) != len(source) || sha256.Sum256(copyBytes) != sha256.Sum256(source) ||
		journal.Snapshot.SHA256 != fmt.Sprintf("%x", sha256.Sum256(source)) || journal.Snapshot.Identity == journal.Source {
		t.Fatal("snapshot did not preserve exact source bytes and independent identity")
	}
	stored, err := loadExportJournal(root, domain.ID("work"), journal.ID)
	if err != nil || stored.Phase != ExportSnapshotReady || stored.Snapshot == nil {
		t.Fatalf("durable snapshot state = %#v, %v", stored, err)
	}
}

func TestCreateExportSnapshotRejectsRunningBackendBeforeTransaction(t *testing.T) {
	root, stopped := stoppedLaunchFixture(t)
	if _, err := CreateExportSnapshot(context.Background(), root, domain.ID("work"), testVolumeID, t.TempDir(), []string{"project/report.txt"}, stoppedObserver{state: backend.ObjectRunning, object: stopped.Backend.ObjectID}); err == nil {
		t.Fatal("running backend admitted for offline snapshot")
	}
	if _, err := os.Stat(filepath.Join(root, "exports")); !os.IsNotExist(err) {
		t.Fatalf("failed admission created export state: %v", err)
	}
}

func TestInterruptedExportCopyRetainsPendingAndJournal(t *testing.T) {
	root, stopped := stoppedLaunchFixture(t)
	parent := t.TempDir()
	if err := os.Chmod(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, err := createExportSnapshot(ctx, root, domain.ID("work"), testVolumeID, parent, []string{"project/report.txt"}, stoppedObserver{state: backend.ObjectStopped, object: stopped.Backend.ObjectID},
		func(guarded context.Context, _ string, _ workspaceformat.Request, _ *os.File, dir *os.Root, _ ExportJournal) (ExportSnapshot, error) {
			partial, err := dir.OpenFile("snapshot.raw", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
			if err != nil {
				return ExportSnapshot{}, err
			}
			if _, err := partial.Write([]byte("partial")); err != nil {
				return ExportSnapshot{}, err
			}
			if err := partial.Close(); err != nil {
				return ExportSnapshot{}, err
			}
			cancel()
			<-guarded.Done()
			return ExportSnapshot{}, guarded.Err()
		})
	if err == nil {
		t.Fatalf("copy interruption = %v", err)
	}
	volume, err := LoadRecord(root, domain.ID("work"), testVolumeID)
	if err != nil || volume.Pending == nil || volume.Pending.Kind != "export-snapshot" || volume.Use != nil {
		t.Fatalf("interrupted copy lost blocking marker: %#v, %v", volume, err)
	}
	journal, err := loadExportJournal(root, domain.ID("work"), volume.Pending.ID)
	if err != nil || journal.Phase != ExportCopying || journal.Snapshot != nil {
		t.Fatalf("interrupted journal = %#v, %v", journal, err)
	}
	if partial, err := os.ReadFile(filepath.Join(root, journal.SnapshotPath)); err != nil || string(partial) != "partial" {
		t.Fatalf("interrupted private copy = %q, %v", partial, err)
	}
	if _, err := PrepareSessionStart(context.Background(), root, domain.ID("work"), stopped, testGeneration, stoppedObserver{state: backend.ObjectStopped, object: stopped.Backend.ObjectID}); err == nil {
		t.Fatal("session started over interrupted export copy")
	}
}

func TestExportSourceRecheckRejectsChangedBytes(t *testing.T) {
	root, _ := stoppedLaunchFixture(t)
	volume, err := LoadRecord(root, domain.ID("work"), testVolumeID)
	if err != nil {
		t.Fatal(err)
	}
	request := workspaceformat.Request{Domain: volume.Domain, VolumeID: volume.VolumeID, FilesystemUUID: volume.FilesystemUUID, SizeBytes: volume.SizeBytes}
	source, _, err := workspaceformat.Admit(root, request)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	before, err := os.ReadFile(filepath.Join(root, "volumes", testVolumeID+".raw"))
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(before)
	writer, err := os.OpenFile(filepath.Join(root, "volumes", testVolumeID+".raw"), os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.WriteAt([]byte{0x42}, 4096); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := verifyExportSource(context.Background(), root, request, source, *volume.Disk, digest[:]); err == nil {
		t.Fatal("changed source bytes matched copied digest")
	}
}
