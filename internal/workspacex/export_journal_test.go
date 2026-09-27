package workspacex

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/weshofmann/boxwarden/internal/domain"
	"github.com/weshofmann/boxwarden/internal/lock"
)

func testExportJournal(parent string) ExportJournal {
	return ExportJournal{
		Version: 1, ID: testGeneration, Domain: domain.ID("work"),
		VolumeID: testVolumeID, SessionID: testSessionID,
		SessionName: "dev", BackendObject: "bw-work-dev",
		FilesystemUUID: testVolumeID, SizeBytes: 64 << 20,
		Source:            DiskIdentity{Device: 1, Inode: 2},
		SnapshotPath:      filepath.Join("exports", testGeneration, "snapshot.raw"),
		DestinationParent: parent, Destination: DiskIdentity{Device: 3, Inode: 4},
		Selected: []string{"project/report.txt"}, Phase: ExportCopying,
	}
}

func TestExportJournalPersistsExactTransactionAndPhase(t *testing.T) {
	root := privateRoot(t)
	parent := t.TempDir()
	journal := testExportJournal(parent)
	if err := createExportJournal(root, journal); err != nil {
		t.Fatal(err)
	}
	got, err := loadExportJournal(root, domain.ID("work"), journal.ID)
	if err != nil || got.ID != journal.ID || got.Phase != ExportCopying || got.VolumeID != journal.VolumeID {
		t.Fatalf("loaded journal = %#v, %v", got, err)
	}
	if err := createExportJournal(root, journal); err == nil {
		t.Fatal("existing transaction was overwritten")
	}
	if _, err := loadExportJournal(root, domain.ID("personal"), journal.ID); err == nil {
		t.Fatal("cross-domain transaction was admitted")
	}
	ready := journal
	ready.Phase = ExportSnapshotReady
	ready.Snapshot = &ExportSnapshot{Identity: DiskIdentity{Device: 3, Inode: 5}, SHA256: strings.Repeat("a", 64)}
	changedBinding := ready
	changedBinding.DestinationParent = t.TempDir()
	if err := advanceExportJournal(context.Background(), root, journal, changedBinding); err == nil {
		t.Fatal("transaction destination changed during snapshot publication")
	}
	if err := advanceExportJournal(context.Background(), root, journal, ready); err != nil {
		t.Fatal(err)
	}
	if err := advanceExportJournal(context.Background(), root, ready, journal); err == nil {
		t.Fatal("snapshot-ready journal regressed to copying")
	}
	got, err = loadExportJournal(root, domain.ID("work"), journal.ID)
	if err != nil || got.Phase != ExportSnapshotReady || got.Snapshot == nil || got.Snapshot.SHA256 != ready.Snapshot.SHA256 {
		t.Fatalf("advanced journal = %#v, %v", got, err)
	}
}

func TestExportJournalRejectsCorruptRecordAndUnsafePath(t *testing.T) {
	root := privateRoot(t)
	journal := testExportJournal(t.TempDir())
	if err := createExportJournal(root, journal); err != nil {
		t.Fatal(err)
	}
	name := filepath.Join(root, "exports", journal.ID+".json")
	raw, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	for label, corrupt := range map[string]string{
		"duplicate field": strings.Replace(string(raw), `"version":1`, `"version":1,"version":1`, 1),
		"unknown field":   strings.TrimSpace(string(raw))[:len(strings.TrimSpace(string(raw)))-1] + `,"extra":true}`,
		"trailing object": string(raw) + `{}`,
	} {
		t.Run(label, func(t *testing.T) {
			if err := os.WriteFile(name, []byte(corrupt), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := loadExportJournal(root, domain.ID("work"), journal.ID); err == nil {
				t.Fatal("corrupt journal accepted")
			}
		})
	}
	journal.Selected = []string{"../escape"}
	if err := createExportJournal(privateRoot(t), journal); err == nil {
		t.Fatal("unsafe selected path accepted")
	}
	journal.Selected = []string{"Project/report.txt", "project/REPORT.txt"}
	if err := createExportJournal(privateRoot(t), journal); err == nil {
		t.Fatal("case-colliding selections accepted")
	}
	journal.Selected = []string{"project/report.txt"}
	journal.SnapshotPath = "exports/other/snapshot.raw"
	if err := createExportJournal(privateRoot(t), journal); err == nil {
		t.Fatal("unbound snapshot path accepted")
	}
}

func TestExportJournalRejectsHardlinkedState(t *testing.T) {
	root := privateRoot(t)
	journal := testExportJournal(t.TempDir())
	if err := createExportJournal(root, journal); err != nil {
		t.Fatal(err)
	}
	name := filepath.Join(root, "exports", journal.ID+".json")
	if err := os.Link(name, filepath.Join(root, "exports", "other.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := loadExportJournal(root, journal.Domain, journal.ID); err == nil {
		t.Fatal("hardlinked journal accepted")
	}
}

func TestExportJournalConcurrentAdvanceAcceptsOneSnapshot(t *testing.T) {
	root := privateRoot(t)
	journal := testExportJournal(t.TempDir())
	if err := createExportJournal(root, journal); err != nil {
		t.Fatal(err)
	}
	var wait sync.WaitGroup
	results := make(chan error, 2)
	for _, inode := range []uint64{5, 6} {
		wait.Add(1)
		go func(inode uint64) {
			defer wait.Done()
			ready := journal
			ready.Phase = ExportSnapshotReady
			ready.Snapshot = &ExportSnapshot{Identity: DiskIdentity{Device: 3, Inode: inode}, SHA256: strings.Repeat("a", 64)}
			results <- advanceExportJournal(context.Background(), root, journal, ready)
		}(inode)
	}
	wait.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("concurrent snapshot publications succeeded %d times, want one", successes)
	}
}

func TestExportJournalAdvanceWaitsForExactTransactionLock(t *testing.T) {
	root := privateRoot(t)
	journal := testExportJournal(t.TempDir())
	if err := createExportJournal(root, journal); err != nil {
		t.Fatal(err)
	}
	held, err := lock.Acquire(context.Background(), root, "export-work-"+journal.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Release()
	ready := journal
	ready.Phase = ExportSnapshotReady
	ready.Snapshot = &ExportSnapshot{Identity: DiskIdentity{Device: 3, Inode: 5}, SHA256: strings.Repeat("a", 64)}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Millisecond)
	defer cancel()
	if err := advanceExportJournal(ctx, root, journal, ready); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("advance bypassed held journal lock: %v", err)
	}
	current, err := loadExportJournal(root, journal.Domain, journal.ID)
	if err != nil || current.Phase != ExportCopying {
		t.Fatalf("blocked advance changed phase: %#v, %v", current, err)
	}
}

func TestExportJournalEncodingCountsFinalNewline(t *testing.T) {
	journal := testExportJournal(t.TempDir())
	journal.Selected = []string{""}
	base, err := json.Marshal(journal)
	if err != nil {
		t.Fatal(err)
	}
	journal.Selected[0] = strings.Repeat("a", maxExportJournalBytes-len(base)-1)
	encoded, err := encodeExportJournal(journal)
	if err != nil || len(encoded) != maxExportJournalBytes {
		t.Fatalf("exact-limit encoded journal = %d, %v", len(encoded), err)
	}
	journal.Selected[0] += "a"
	if _, err := encodeExportJournal(journal); err == nil {
		t.Fatal("final newline exceeded journal limit")
	}
}

func TestExportJournalRejectsNilTransactionID(t *testing.T) {
	journal := testExportJournal(t.TempDir())
	journal.ID = "00000000-0000-0000-0000-000000000000"
	journal.SnapshotPath = filepath.Join("exports", journal.ID, "snapshot.raw")
	if err := createExportJournal(privateRoot(t), journal); err == nil {
		t.Fatal("nil receiver transaction admitted")
	}
}
