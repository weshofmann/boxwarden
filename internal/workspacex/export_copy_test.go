package workspacex

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/weshofmann/boxwarden/internal/backend"
	"github.com/weshofmann/boxwarden/internal/domain"
	"github.com/weshofmann/boxwarden/internal/session"
	"github.com/weshofmann/boxwarden/internal/workspaceformat"
)

// Synthetic workflow tests exercise transaction behavior independently of host disk occupancy.
func allowSyntheticExportHeadroom(...*os.File) error          { return nil }
func syntheticExportReceiverReserve(*os.File) (uint64, error) { return 1, nil }

func TestCreateExportSnapshotCopiesStoppedQualifiedVolumeBeforeReleasingPending(t *testing.T) {
	root, stopped := stoppedLaunchFixture(t)
	parent := t.TempDir()
	if err := os.Chmod(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	journal, err := createExportSnapshot(context.Background(), root, domain.ID("work"), testVolumeID, parent, []string{"project/report.txt"}, stoppedObserver{state: backend.ObjectStopped, object: stopped.Backend.ObjectID}, copyExportSnapshot, allowSyntheticExportHeadroom)
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

func TestCreateExportSnapshotRejectsHeadroomBeforeTransaction(t *testing.T) {
	root, stopped := stoppedLaunchFixture(t)
	parent := t.TempDir()
	if err := os.Chmod(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	refused := errors.New("synthetic capacity refusal")
	_, err := createExportSnapshot(context.Background(), root, domain.ID("work"), testVolumeID, parent, []string{"project/report.txt"},
		stoppedObserver{state: backend.ObjectStopped, object: stopped.Backend.ObjectID},
		copyExportSnapshot, func(dirs ...*os.File) error {
			if len(dirs) != 2 {
				t.Fatalf("headroom check saw %d directories, want state and destination", len(dirs))
			}
			return refused
		})
	if !errors.Is(err, refused) {
		t.Fatalf("headroom refusal = %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "exports")); !os.IsNotExist(err) {
		t.Fatalf("headroom refusal created export state: %v", err)
	}
	volume, err := LoadRecord(root, domain.ID("work"), testVolumeID)
	if err != nil || volume.Pending != nil || volume.Use != nil {
		t.Fatalf("headroom refusal reserved workspace: %#v, %v", volume, err)
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

func TestFalseStoppedListingCannotDetachExportOrReleaseLiveWorkspace(t *testing.T) {
	root, running := launchFixture(t, true)
	running.IntendedState = session.StateRunning
	running.Readiness = session.ReadinessRecord{Status: session.ReadinessReady}
	if err := session.SaveRecord(root, domain.ID("work"), running); err != nil {
		t.Fatal(err)
	}
	listedStopped := stoppedObserver{state: backend.ObjectStopped, object: running.Backend.ObjectID}
	if _, err := Detach(context.Background(), root, domain.ID("work"), testVolumeID, string(running.Name), listedStopped); err == nil {
		t.Fatal("false-stopped listing detached a live workspace")
	}
	if _, err := CreateExportSnapshot(context.Background(), root, domain.ID("work"), testVolumeID, t.TempDir(), []string{"project/report.txt"}, listedStopped); err == nil {
		t.Fatal("false-stopped listing admitted offline export of a live workspace")
	}
	volume, err := LoadRecord(root, domain.ID("work"), testVolumeID)
	if err != nil || volume.Use == nil || volume.Attachment == nil {
		t.Fatalf("live workspace binding changed: %#v, %v", volume, err)
	}
	if _, err := ReleaseUseAfterObservedStop(context.Background(), root, domain.ID("work"), testVolumeID, *volume.Use, listedStopped); err == nil {
		t.Fatal("false-stopped listing released a running session's exact Use")
	}
	if _, err := os.Stat(filepath.Join(root, "exports")); !os.IsNotExist(err) {
		t.Fatalf("rejected offline export created transaction state: %v", err)
	}
	persisted, err := LoadRecord(root, domain.ID("work"), testVolumeID)
	if err != nil || persisted.Use == nil || *persisted.Use != *volume.Use || persisted.Attachment == nil || *persisted.Attachment != *volume.Attachment {
		t.Fatalf("rejected storage actions changed live binding: %#v, %v", persisted, err)
	}
}

func TestPendingRebuildBlocksStoppedWorkspaceOperations(t *testing.T) {
	root, stopped := stoppedLaunchFixture(t)
	j := session.RebuildJournal{
		Version: 1, Domain: domain.ID("work"), SessionName: string(stopped.Name), SessionID: stopped.ID,
		OperationID: "7fb25db7-3cc1-4d92-a04c-b60fd05fa421", Phase: session.RebuildReserved,
		OldBackend: stopped.Backend.ObjectID, OldRevision: stopped.GoldenRevision,
		CandidateBackend: "boxwarden-work-7fb25db73cc14d92a04cb60fd05fa421", CandidateRevision: stopped.GoldenRevision,
	}
	raw, err := json.Marshal(j)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "rebuilds"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "rebuilds", string(stopped.Name)+".json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	observer := stoppedObserver{state: backend.ObjectStopped, object: stopped.Backend.ObjectID}
	if _, err := Detach(context.Background(), root, domain.ID("work"), testVolumeID, string(stopped.Name), observer); err == nil {
		t.Fatal("pending rebuild allowed workspace detach")
	}
	if _, err := CreateExportSnapshot(context.Background(), root, domain.ID("work"), testVolumeID, t.TempDir(), []string{"project/report.txt"}, observer); err == nil {
		t.Fatal("pending rebuild allowed offline export")
	}
	if _, err := PrepareSessionStart(context.Background(), root, domain.ID("work"), stopped, testGeneration, observer); err == nil {
		t.Fatal("pending rebuild allowed ordinary workspace start reservation")
	}
	volume, err := LoadRecord(root, domain.ID("work"), testVolumeID)
	if err != nil || volume.Use != nil || volume.Pending != nil || volume.Attachment == nil {
		t.Fatalf("blocked rebuild operations changed workspace: %#v, %v", volume, err)
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
		}, allowSyntheticExportHeadroom)
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

func TestRecoverInterruptedExportCopyRequiresStopAndClearsExactPending(t *testing.T) {
	root, stopped := stoppedLaunchFixture(t)
	parent := t.TempDir()
	if err := os.Chmod(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	_, err := createExportSnapshot(context.Background(), root, domain.ID("work"), testVolumeID, parent, []string{"project/report.txt"}, stoppedObserver{state: backend.ObjectStopped, object: stopped.Backend.ObjectID},
		func(_ context.Context, _ string, _ workspaceformat.Request, _ *os.File, dir *os.Root, _ ExportJournal) (ExportSnapshot, error) {
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
			return ExportSnapshot{}, errors.New("injected copy failure")
		}, allowSyntheticExportHeadroom)
	if err == nil {
		t.Fatal("injected copy failure was accepted")
	}
	volume, err := LoadRecord(root, domain.ID("work"), testVolumeID)
	if err != nil || volume.Pending == nil {
		t.Fatalf("copy did not retain Pending: %#v, %v", volume.Pending, err)
	}
	id := volume.Pending.ID
	foreign := filepath.Join(root, "exports", id, "foreign")
	if err := os.WriteFile(foreign, []byte("unexpected"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := RecoverExportSnapshot(context.Background(), root, domain.ID("work"), id, stoppedObserver{state: backend.ObjectStopped, object: stopped.Backend.ObjectID}); err == nil {
		t.Fatal("recovery removed a transaction directory with unexpected state")
	}
	if _, err := os.Stat(filepath.Join(root, "exports", id, "snapshot.raw")); err != nil {
		t.Fatalf("ambiguous cleanup removed partial copy: %v", err)
	}
	if err := os.Remove(foreign); err != nil {
		t.Fatal(err)
	}
	if _, err := RecoverExportSnapshot(context.Background(), root, domain.ID("work"), id, stoppedObserver{state: backend.ObjectRunning, object: stopped.Backend.ObjectID}); err == nil {
		t.Fatal("recovery cleared copy while backend ran")
	}
	blocked, err := LoadRecord(root, domain.ID("work"), testVolumeID)
	if err != nil || blocked.Pending == nil || blocked.Pending.ID != id {
		t.Fatalf("running-backend rejection cleared Pending: %#v, %v", blocked.Pending, err)
	}
	if _, err := os.Stat(filepath.Join(root, "exports", id, "snapshot.raw")); err != nil {
		t.Fatalf("running-backend rejection removed partial copy: %v", err)
	}
	recovered, err := RecoverExportSnapshot(context.Background(), root, domain.ID("work"), id, stoppedObserver{state: backend.ObjectStopped, object: stopped.Backend.ObjectID})
	if err != nil || recovered.Phase != ExportAborted {
		t.Fatalf("recovery = %#v, %v", recovered, err)
	}
	if _, err := os.Stat(filepath.Join(root, recovered.SnapshotPath)); !os.IsNotExist(err) {
		t.Fatalf("owned partial snapshot retained: %v", err)
	}
	volume, err = LoadRecord(root, domain.ID("work"), testVolumeID)
	if err != nil || volume.Pending != nil || volume.Use != nil {
		t.Fatalf("recovery left blocked or in-use volume: %#v, %v", volume, err)
	}
	if _, err := PrepareSessionStart(context.Background(), root, domain.ID("work"), stopped, testGeneration, stoppedObserver{state: backend.ObjectStopped, object: stopped.Backend.ObjectID}); err != nil {
		t.Fatalf("aborted snapshot did not release volume for start: %v", err)
	}
	if again, err := RecoverExportSnapshot(context.Background(), root, domain.ID("work"), id, stoppedObserver{state: backend.ObjectRunning, object: stopped.Backend.ObjectID}); err != nil || again.Phase != ExportAborted {
		t.Fatalf("idempotent recovery = %#v, %v", again, err)
	}
}

func TestRecoverReadyExportSnapshotRechecksBytesBeforePendingClear(t *testing.T) {
	for _, tamper := range []bool{false, true} {
		t.Run(fmt.Sprintf("tamper=%t", tamper), func(t *testing.T) {
			root, stopped := stoppedLaunchFixture(t)
			parent := t.TempDir()
			if err := os.Chmod(parent, 0o700); err != nil {
				t.Fatal(err)
			}
			observer := stoppedObserver{state: backend.ObjectStopped, object: stopped.Backend.ObjectID}
			journal, err := createExportSnapshot(context.Background(), root, domain.ID("work"), testVolumeID, parent, []string{"project/report.txt"}, observer, copyExportSnapshot, allowSyntheticExportHeadroom)
			if err != nil {
				t.Fatal(err)
			}
			volume, err := LoadRecord(root, domain.ID("work"), testVolumeID)
			if err != nil {
				t.Fatal(err)
			}
			volume.Pending = &Pending{Kind: "export-snapshot", ID: journal.ID}
			if err := saveRecordTransition(root, domain.ID("work"), volume, mutationBeginExportSnapshot, nil); err != nil {
				t.Fatal(err)
			}
			if tamper {
				file, err := os.OpenFile(filepath.Join(root, journal.SnapshotPath), os.O_WRONLY, 0)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := file.WriteAt([]byte{0x42}, 4096); err != nil {
					t.Fatal(err)
				}
				if err := file.Close(); err != nil {
					t.Fatal(err)
				}
			}
			_, err = RecoverExportSnapshot(context.Background(), root, domain.ID("work"), journal.ID, observer)
			if (err != nil) != tamper {
				t.Fatalf("ready recovery err = %v, tamper=%t", err, tamper)
			}
			volume, err = LoadRecord(root, domain.ID("work"), testVolumeID)
			if err != nil || (volume.Pending != nil) != tamper {
				t.Fatalf("ready recovery marker = %#v, %v", volume.Pending, err)
			}
		})
	}
}

func TestRecoverExportCopyAfterCleanupBeforeAbortJournal(t *testing.T) {
	root, stopped := stoppedLaunchFixture(t)
	parent := t.TempDir()
	if err := os.Chmod(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	observer := stoppedObserver{state: backend.ObjectStopped, object: stopped.Backend.ObjectID}
	_, err := createExportSnapshot(context.Background(), root, domain.ID("work"), testVolumeID, parent, []string{"project/report.txt"}, observer,
		func(_ context.Context, _ string, _ workspaceformat.Request, _ *os.File, dir *os.Root, _ ExportJournal) (ExportSnapshot, error) {
			partial, err := dir.OpenFile("snapshot.raw", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
			if err != nil {
				return ExportSnapshot{}, err
			}
			if err := partial.Close(); err != nil {
				return ExportSnapshot{}, err
			}
			return ExportSnapshot{}, errors.New("injected copy failure")
		}, allowSyntheticExportHeadroom)
	if err == nil {
		t.Fatal("injected copy failure was accepted")
	}
	volume, err := LoadRecord(root, domain.ID("work"), testVolumeID)
	if err != nil || volume.Pending == nil {
		t.Fatalf("copy marker = %#v, %v", volume.Pending, err)
	}
	id := volume.Pending.ID
	journal, err := loadExportJournal(root, domain.ID("work"), id)
	if err != nil {
		t.Fatal(err)
	}
	// Model interruption at the recovery boundary immediately after exact
	// cleanup, before the durable aborted journal update.
	if err := removeExactPartialSnapshot(root, journal); err != nil {
		t.Fatal(err)
	}
	journal, err = loadExportJournal(root, domain.ID("work"), id)
	if err != nil || journal.Phase != ExportCopying {
		t.Fatalf("cleanup crash journal = %#v, %v", journal, err)
	}
	if _, err := os.Stat(filepath.Join(root, "exports", id)); !os.IsNotExist(err) {
		t.Fatalf("partial directory survived cleanup: %v", err)
	}
	volume, err = LoadRecord(root, domain.ID("work"), testVolumeID)
	if err != nil || volume.Pending == nil || volume.Pending.ID != id {
		t.Fatalf("cleanup crash cleared marker: %#v, %v", volume.Pending, err)
	}
	recovered, err := RecoverExportSnapshot(context.Background(), root, domain.ID("work"), id, observer)
	if err != nil || recovered.Phase != ExportAborted {
		t.Fatalf("retry after cleanup = %#v, %v", recovered, err)
	}
	volume, err = LoadRecord(root, domain.ID("work"), testVolumeID)
	if err != nil || volume.Pending != nil {
		t.Fatalf("retry retained marker: %#v, %v", volume.Pending, err)
	}
	// Model a crash after the aborted journal is durable but before the
	// exact Pending marker is cleared. The retry must finish that transition.
	volume.Pending = &Pending{Kind: "export-snapshot", ID: id}
	if err := saveRecordTransition(root, domain.ID("work"), volume, mutationBeginExportSnapshot, nil); err != nil {
		t.Fatal(err)
	}
	recovered, err = RecoverExportSnapshot(context.Background(), root, domain.ID("work"), id, observer)
	if err != nil || recovered.Phase != ExportAborted {
		t.Fatalf("retry after aborted journal = %#v, %v", recovered, err)
	}
	volume, err = LoadRecord(root, domain.ID("work"), testVolumeID)
	if err != nil || volume.Pending != nil {
		t.Fatalf("aborted retry retained marker: %#v, %v", volume.Pending, err)
	}
}
