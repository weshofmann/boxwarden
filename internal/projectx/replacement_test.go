package projectx

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/weshofmann/boxwarden/internal/session"
)

func replacementFixture(t *testing.T) (string, Record, session.RebuildJournal) {
	t.Helper()
	root := privateRoot(t)
	before := fixtureRecord()
	before.SessionID = "22222233-4455-6677-8899-aabbccddeeff"
	before.BackendObject = "boxwarden-work-old"
	before.Initialized = true
	before.ImportID = "33332233-4455-6677-8899-aabbccddeeff"
	before.ImportSource = "/private/source"
	before.Imported = true
	if err := Create(root, "work", before); err != nil {
		t.Fatal(err)
	}
	operation := "44442233-4455-6677-8899-aabbccddeeff"
	j := session.RebuildJournal{Version: 1, Domain: "work", SessionName: before.Name, SessionID: before.SessionID, OperationID: operation, Phase: session.RebuildCloned, OldBackend: before.BackendObject, OldRevision: before.Base, CandidateBackend: "boxwarden-work-44442233445566778899aabbccddeeff", CandidateRevision: "golden-r2"}
	return root, before, j
}

func TestReplacementChangesOnlySystemBindingAndRetainsHistory(t *testing.T) {
	root, before, j := replacementFixture(t)
	intent, err := BeginReplacement(root, "work", before, j)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := Load(root, "work", "dev"); got != before {
		t.Fatal("preparation changed bookmark")
	}
	if _, err := BeginReplacement(root, "work", before, j); err != nil {
		t.Fatal(err)
	}
	if _, err := CompleteReplacement(root, "work", intent); err != nil {
		t.Fatal(err)
	}
	got, err := Load(root, "work", "dev")
	if err != nil {
		t.Fatal(err)
	}
	want := before
	want.Base = j.CandidateRevision
	want.BackendObject = j.CandidateBackend
	if got != want {
		t.Fatalf("workspace/import/name/session changed: %+v", got)
	}
	if err := Save(root, "work", before); err == nil {
		t.Fatal("ordinary save reverted replacement")
	}
	if _, err := LoadReplacement(root, "work", "dev"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("pending intent retained: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "projects", ".replacement-history-"+j.OperationID+".json")); err != nil {
		t.Fatal(err)
	}
}

func TestReplacementRejectsForeignWitnessAndChangedBookmark(t *testing.T) {
	for _, change := range []string{"session", "old-backend", "old-base", "domain", "candidate", "phase", "recipe"} {
		t.Run(change, func(t *testing.T) {
			root, before, j := replacementFixture(t)
			switch change {
			case "session":
				j.SessionID = "55552233-4455-6677-8899-aabbccddeeff"
			case "old-backend":
				j.OldBackend = "foreign"
			case "old-base":
				j.OldRevision = "foreign"
			case "domain":
				j.Domain = "alpha"
			case "candidate":
				j.CandidateBackend = "foreign"
			case "phase":
				j.Phase = session.RebuildReserved
			case "recipe":
				j.CandidateIntentDigest = "foreign"
			}
			if _, err := BeginReplacement(root, "work", before, j); err == nil {
				t.Fatal("accepted foreign candidate witness")
			}
		})
	}
	root, before, j := replacementFixture(t)
	intent, err := BeginReplacement(root, "work", before, j)
	if err != nil {
		t.Fatal(err)
	}
	changed := before
	changed.ImportSource = "/changed/source"
	if err := os.WriteFile(recordPath(root), append(mustJSON(t, changed), '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := CompleteReplacement(root, "work", intent); err == nil {
		t.Fatal("adopted changed bookmark")
	}
}

func TestReplacementFinalUnlinkSyncFailureSettlesExactRetry(t *testing.T) {
	root, before, j := replacementFixture(t)
	intent, err := BeginReplacement(root, "work", before, j)
	if err != nil {
		t.Fatal(err)
	}
	priorSync := syncProjectDirectory
	t.Cleanup(func() { syncProjectDirectory = priorSync })
	failed := false
	settledSyncs := 0
	syncProjectDirectory = func(dir *os.Root) error {
		_, err := dir.Lstat(replacementName(before.Name))
		if errors.Is(err, os.ErrNotExist) && !failed {
			failed = true
			return errors.New("injected final unlink directory sync failure")
		}
		if errors.Is(err, os.ErrNotExist) {
			settledSyncs++
		}
		return priorSync(dir)
	}
	if _, err := CompleteReplacement(root, "work", intent); err == nil || !failed {
		t.Fatalf("final unlink failure not observed: %v", err)
	}
	if _, err := LoadReplacement(root, "work", before.Name); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("intent was not unlinked: %v", err)
	}
	if got, err := Load(root, "work", before.Name); err != nil || got != intent.Next() {
		t.Fatalf("cutover not visible: %+v %v", got, err)
	}
	bookmarkBytes, err := os.ReadFile(recordPath(root))
	if err != nil {
		t.Fatal(err)
	}
	historyPath := filepath.Join(root, "projects", replacementHistory(intent))
	historyBytes, err := os.ReadFile(historyPath)
	if err != nil {
		t.Fatal(err)
	}
	recovered, err := LoadCompletedReplacement(root, "work", before.Name)
	if err != nil || recovered != intent {
		t.Fatalf("cannot reconstruct exact completed intent: %+v %v", recovered, err)
	}
	if got, err := CompleteReplacement(root, "work", recovered); err != nil || got != intent.Next() {
		t.Fatalf("exact final-unlink retry cannot settle: %+v %v", got, err)
	}
	if settledSyncs != 1 {
		t.Fatalf("completed retry did not settle the unlink sync: %d", settledSyncs)
	}
	afterBookmark, err := os.ReadFile(recordPath(root))
	if err != nil {
		t.Fatal(err)
	}
	afterHistory, err := os.ReadFile(historyPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(bookmarkBytes, afterBookmark) || !bytes.Equal(historyBytes, afterHistory) {
		t.Fatal("completed retry rewrote bookmark or history")
	}
}

func TestLoadCompletedReplacementBoundToCurrentBookmarkAndPrivateExactHistory(t *testing.T) {
	for _, change := range []string{"none", "current", "foreign-domain", "foreign-operation", "foreign-name", "nonprivate", "symlink", "corrupt", "missing-history"} {
		t.Run(change, func(t *testing.T) {
			root, before, j := replacementFixture(t)
			intent, err := BeginReplacement(root, "work", before, j)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := CompleteReplacement(root, "work", intent); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(root, "projects", replacementHistory(intent))
			changed := intent
			switch change {
			case "current":
				next := intent.Next()
				next.Base = "changed-base"
				if err := os.WriteFile(recordPath(root), append(mustJSON(t, next), '\n'), 0600); err != nil {
					t.Fatal(err)
				}
			case "foreign-domain":
				changed.Before.Domain = "alpha"
			case "foreign-operation":
				changed.OperationID = "55552233-4455-6677-8899-aabbccddeeff"
				changed.BackendObject = "boxwarden-work-55552233445566778899aabbccddeeff"
			case "foreign-name":
				changed.Before.Name = "other"
			case "nonprivate":
				if err := os.Chmod(path, 0644); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				if err := os.Rename(path, path+".original"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(path+".original", path); err != nil {
					t.Fatal(err)
				}
			case "corrupt":
				if err := os.WriteFile(path, []byte("{}"), 0600); err != nil {
					t.Fatal(err)
				}
			case "missing-history":
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
			}
			if changed != intent {
				if err := os.WriteFile(path, append(mustJSON(t, changed), '\n'), 0600); err != nil {
					t.Fatal(err)
				}
			}
			got, err := LoadCompletedReplacement(root, "work", before.Name)
			if change == "none" {
				if err != nil || got != intent {
					t.Fatalf("completed reader: %+v %v", got, err)
				}
			} else if err == nil {
				t.Fatalf("completed reader admitted %s", change)
			}
			if change != "none" {
				if _, err := CompleteReplacement(root, "work", intent); err == nil {
					t.Fatalf("settlement admitted %s", change)
				}
			}
		})
	}
}

func TestLoadCompletedReplacementSelectsOnlyLatestExactBookmark(t *testing.T) {
	root, before, j := replacementFixture(t)
	first, err := BeginReplacement(root, "work", before, j)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := CompleteReplacement(root, "work", first); err != nil {
		t.Fatal(err)
	}
	next := first.Next()
	j.OldBackend = next.BackendObject
	j.OldRevision = next.Base
	j.OperationID = "55552233-4455-6677-8899-aabbccddeeff"
	j.CandidateBackend = "boxwarden-work-55552233445566778899aabbccddeeff"
	j.CandidateRevision = "golden-r3"
	latest, err := BeginReplacement(root, "work", next, j)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := LoadCompletedReplacement(root, "work", next.Name); err == nil {
		t.Fatalf("pending replacement exposed as completed: %v", err)
	}
	if _, err := CompleteReplacement(root, "work", latest); err != nil {
		t.Fatal(err)
	}
	if got, err := LoadCompletedReplacement(root, "work", next.Name); err != nil || got != latest {
		t.Fatalf("latest reader: %+v %v", got, err)
	}
	if _, err := CompleteReplacement(root, "work", first); err == nil {
		t.Fatal("old completed operation reverted newer binding")
	}
}

func TestLoadCompletedReplacementBoundsHistoryScan(t *testing.T) {
	root, before, _ := replacementFixture(t)
	for i := 0; i < 1024; i++ {
		if err := os.WriteFile(filepath.Join(root, "projects", fmt.Sprintf(".replacement-history-%d.json", i)), []byte("{}"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := LoadCompletedReplacement(root, "work", before.Name); err == nil || !strings.Contains(err.Error(), "exceeds 1024") {
		t.Fatalf("unbounded history reader: %v", err)
	}
}
