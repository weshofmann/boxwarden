package workspacex

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func writeExportListFixture(t *testing.T, root string, journal ExportJournal) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, "exports"), 0o700); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(journal)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "exports", journal.ID+".json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestListExportJournalsReadsRecordedPhasesWithoutSnapshotOrDestination(t *testing.T) {
	root := privateRoot(t)
	var want []ExportJournal
	for i, phase := range []ExportPhase{ExportCopying, ExportAborted, ExportSnapshotReady, ExportInspected, ExportPublished} {
		j := testExportJournal("/unavailable/synthetic-destination")
		j.ID = fmt.Sprintf("00112233-4455-4677-8899-%012d", i+1)
		j.SnapshotPath = filepath.Join("exports", j.ID, "snapshot.raw")
		j.Phase = phase
		if phase != ExportCopying && phase != ExportAborted {
			j.Snapshot = &ExportSnapshot{Identity: DiskIdentity{Device: 3, Inode: 5}, SHA256: strings.Repeat("a", 64)}
		}
		writeExportListFixture(t, root, j)
		want = append(want, j)
	}
	foreign := want[0]
	foreign.ID = "00112233-4455-4677-8899-000000000006"
	foreign.SnapshotPath = filepath.Join("exports", foreign.ID, "snapshot.raw")
	foreign.VolumeID = "aabbccdd-4455-4677-8899-000000000006"
	writeExportListFixture(t, root, foreign)
	// An interrupted writer's known private temporary file is metadata only;
	// incomplete JSON is never presented as an authoritative transaction.
	temporary := filepath.Join(root, "exports", want[0].ID+".json.tmp-"+strings.Repeat("a", 32))
	if err := os.WriteFile(temporary, []byte("partial"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "exports", want[0].ID), 0o700); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(temporary)
	if err != nil {
		t.Fatal(err)
	}
	got, err := ListExportJournals(t.Context(), root, "work", testVolumeID)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("recorded listing: %#v %v", got, err)
	}
	if after, err := os.ReadFile(temporary); err != nil || string(after) != string(before) {
		t.Fatalf("temporary rewritten: %q %v", after, err)
	}
	for _, j := range want {
		if stored, err := loadExportJournal(root, "work", j.ID); err != nil || !reflect.DeepEqual(stored, j) {
			t.Fatalf("listing changed journal: %#v %v", stored, err)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "locks")); !os.IsNotExist(err) {
		t.Fatalf("listing created locks: %v", err)
	}
}

func TestListExportJournalsAbsentRegistryAndUnavailableRoot(t *testing.T) {
	root := privateRoot(t)
	got, err := ListExportJournals(t.Context(), root, "work", testVolumeID)
	if err != nil || len(got) != 0 {
		t.Fatalf("absent registry: %#v %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(root, "exports")); !os.IsNotExist(err) {
		t.Fatalf("listing created registry: %v", err)
	}
	if _, err := ListExportJournals(t.Context(), filepath.Join(root, "missing"), "work", testVolumeID); err == nil {
		t.Fatal("unavailable root became empty registry")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := ListExportJournals(ctx, root, "work", testVolumeID); err == nil {
		t.Fatal("cancelled listing succeeded")
	}
}

func TestListExportJournalsFailsClosedOnUnsafeOrCorruptMetadata(t *testing.T) {
	for _, scenario := range []string{"corrupt", "foreign domain", "unknown", "symlink journal", "hardlink journal", "shared journal", "symlink directory", "shared directory", "symlink temporary", "oversized temporary"} {
		t.Run(scenario, func(t *testing.T) {
			root := privateRoot(t)
			j := testExportJournal("/unavailable/synthetic-destination")
			if scenario == "foreign domain" {
				j.Domain = "alpha"
			}
			writeExportListFixture(t, root, j)
			path := filepath.Join(root, "exports", j.ID+".json")
			switch scenario {
			case "corrupt":
				if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
					t.Fatal(err)
				}
			case "unknown":
				if err := os.WriteFile(filepath.Join(root, "exports", "unknown"), nil, 0o600); err != nil {
					t.Fatal(err)
				}
			case "symlink journal":
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink("missing", path); err != nil {
					t.Fatal(err)
				}
			case "hardlink journal":
				if err := os.Link(path, filepath.Join(root, "linked")); err != nil {
					t.Fatal(err)
				}
			case "shared journal":
				if err := os.Chmod(path, 0o644); err != nil {
					t.Fatal(err)
				}
			case "symlink directory":
				if err := os.Symlink(privateRoot(t), filepath.Join(root, "exports", j.ID)); err != nil {
					t.Fatal(err)
				}
			case "shared directory":
				if err := os.Mkdir(filepath.Join(root, "exports", j.ID), 0o755); err != nil {
					t.Fatal(err)
				}
			case "symlink temporary":
				if err := os.Symlink("missing", path+".tmp-"+strings.Repeat("a", 32)); err != nil {
					t.Fatal(err)
				}
			case "oversized temporary":
				if err := os.WriteFile(path+".tmp-"+strings.Repeat("a", 32), make([]byte, (64<<10)+1), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if got, err := ListExportJournals(t.Context(), root, "work", testVolumeID); err == nil || got != nil {
				t.Fatalf("unsafe registry returned partial or accepted data: %#v %v", got, err)
			}
		})
	}
}

func TestListExportJournalsRegistryEntryAndJournalBounds(t *testing.T) {
	root := privateRoot(t)
	j := testExportJournal("/unavailable/synthetic-destination")
	writeExportListFixture(t, root, j)
	for i := 0; i < 1024; i++ {
		temporary := filepath.Join(root, "exports", j.ID+".json.tmp-"+fmt.Sprintf("%032x", i))
		if err := os.WriteFile(temporary, nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if got, err := ListExportJournals(t.Context(), root, "work", testVolumeID); err != nil || len(got) != 1 {
		t.Fatalf("known temporaries hid journal: %d %v", len(got), err)
	}
	for i := 1024; i < 4096; i++ {
		if err := os.WriteFile(filepath.Join(root, "exports", j.ID+".json.tmp-"+fmt.Sprintf("%032x", i)), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := ListExportJournals(t.Context(), root, "work", testVolumeID); err == nil {
		t.Fatal("unbounded registry entries admitted")
	}
	other := privateRoot(t)
	for i := 0; i < 1025; i++ {
		j.ID = fmt.Sprintf("00112233-4455-4677-8899-%012d", i+1)
		j.SnapshotPath = filepath.Join("exports", j.ID, "snapshot.raw")
		writeExportListFixture(t, other, j)
	}
	if _, err := ListExportJournals(t.Context(), other, "work", testVolumeID); err == nil {
		t.Fatal("unbounded journals admitted")
	}
}
