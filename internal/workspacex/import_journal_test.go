package workspacex

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/weshofmann/boxwarden/internal/domain"
	"github.com/weshofmann/boxwarden/internal/importx"
)

const testImportID = "e03fa7a8-fc96-43fc-b654-5826920dc246"

func importJournalFixture(t *testing.T) (string, ImportJournal) {
	t.Helper()
	stateRoot := t.TempDir()
	if err := os.Chmod(stateRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	if err := root.Mkdir("imports", 0o700); err != nil {
		t.Fatal(err)
	}
	if err := root.Close(); err != nil {
		t.Fatal(err)
	}
	snapshot := writeCapturedImportFixture(t, stateRoot, testImportID)
	journal := ImportJournal{Version: 1, ID: testImportID, Domain: domain.ID("alpha"),
		SessionID: "69d67808-a93d-4dd5-a813-dfb1ff8e6de9", SessionName: "sandboxa",
		BackendObject: "boxwarden-alpha-69d67808a93d4dd5a813dfb1ff8e6de9", Generation: "01d13d1e-51e1-4818-b8ef-62e2af16865e",
		VolumeID: "65bb9dbf-1f87-478b-a7f2-4133b9e5b3b6", FilesystemUUID: "18ab9caf-4491-4983-8159-1ca1c38ed28c",
		MountPath: "/home/boxwarden/workspaces/project", SourceDigest: snapshot.Digest, FileCount: snapshot.FileCount,
		TotalBytes: snapshot.TotalBytes, Phase: ImportCaptured}
	return stateRoot, journal
}

// Import journal and transfer tests admit captured bytes without exercising
// the production disk reserve. CaptureSource has its own bounded tests.
func writeCapturedImportFixture(t *testing.T, stateRoot, transactionID string) importx.Snapshot {
	t.Helper()
	parent := filepath.Join(stateRoot, "imports")
	directory := filepath.Join(parent, transactionID)
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	content := []byte("synthetic\n")
	if err := os.WriteFile(filepath.Join(directory, "project.txt"), content, 0o600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(content)
	manifest, err := json.Marshal(struct {
		Version int             `json:"version"`
		Entries []importx.Entry `json:"entries"`
	}{Version: 1, Entries: []importx.Entry{{Path: "project.txt", Kind: "file", Size: int64(len(content)), SHA256: hex.EncodeToString(digest[:])}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, ".boxwarden-import-manifest.json"), append(manifest, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	snapshot, err := importx.InspectSnapshot(parent, transactionID)
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func TestImportJournalBindsCapturedBytesAndExactTransitions(t *testing.T) {
	stateRoot, journal := importJournalFixture(t)
	if err := createImportJournal(stateRoot, journal); err != nil {
		t.Fatal(err)
	}
	loaded, err := loadImportJournal(stateRoot, journal.Domain, journal.ID)
	if err != nil || loaded != journal {
		t.Fatalf("load captured import: %#v, %v", loaded, err)
	}
	if err := createImportJournal(stateRoot, journal); err != nil {
		t.Fatalf("exact captured transaction was not idempotent: %v", err)
	}
	transferring := journal
	transferring.Phase = ImportTransferring
	if err := advanceImportJournal(context.Background(), stateRoot, journal, transferring); err != nil {
		t.Fatal(err)
	}
	if err := advanceImportJournal(context.Background(), stateRoot, journal, transferring); err != nil {
		t.Fatalf("exact transition retry was not idempotent: %v", err)
	}
	loaded, err = loadImportJournal(stateRoot, journal.Domain, journal.ID)
	if err != nil || loaded != transferring {
		t.Fatalf("load transferring import: %#v, %v", loaded, err)
	}
}

func TestImportJournalRejectsVerifiedPhaseWithoutDistinctExport(t *testing.T) {
	root, captured := importJournalFixture(t)
	if err := createImportJournal(root, captured); err != nil {
		t.Fatal(err)
	}
	verified := captured
	verified.Phase = ImportVerified
	if err := advanceImportJournal(t.Context(), root, captured, verified); err == nil {
		t.Fatal("verified import accepted without a published export identity")
	}
	verified.ExportID = testImportExportID
	if err := advanceImportJournal(t.Context(), root, captured, verified); err == nil {
		t.Fatal("captured import skipped the transferring phase")
	}
	transferring := captured
	transferring.Phase = ImportTransferring
	if err := advanceImportJournal(t.Context(), root, captured, transferring); err != nil {
		t.Fatal(err)
	}
	verified.ExportID = captured.ID
	if err := advanceImportJournal(t.Context(), root, transferring, verified); err == nil {
		t.Fatal("import adopted its own transaction as export evidence")
	}
}

func TestImportJournalRecoversUncertainParentSync(t *testing.T) {
	stateRoot, journal := importJournalFixture(t)
	previous := syncImportDirectory
	defer func() { syncImportDirectory = previous }()
	failed := false
	syncImportDirectory = func(root *os.Root) error {
		if !failed {
			failed = true
			return errors.New("injected parent sync failure")
		}
		return previous(root)
	}
	if err := createImportJournal(stateRoot, journal); err == nil {
		t.Fatal("uncertain journal publication reported success")
	}
	loaded, err := loadImportJournal(stateRoot, journal.Domain, journal.ID)
	if err != nil || loaded != journal {
		t.Fatalf("published transaction was not visible for retry: %#v, %v", loaded, err)
	}
	if err := createImportJournal(stateRoot, journal); err != nil {
		t.Fatalf("exact transaction retry did not sync directory: %v", err)
	}
	transferring := journal
	transferring.Phase = ImportTransferring
	failed = false
	if err := advanceImportJournal(context.Background(), stateRoot, journal, transferring); err == nil {
		t.Fatal("uncertain journal transition reported success")
	}
	snapshotFile := filepath.Join(stateRoot, "imports", journal.ID, "project.txt")
	if err := os.WriteFile(snapshotFile, []byte("changed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := advanceImportJournal(context.Background(), stateRoot, journal, transferring); err == nil {
		t.Fatal("changed snapshot was admitted on exact transition retry")
	}
	if err := os.WriteFile(snapshotFile, []byte("synthetic\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := advanceImportJournal(context.Background(), stateRoot, journal, transferring); err != nil {
		t.Fatalf("exact transition retry did not sync directory: %v", err)
	}
}

func TestImportJournalRejectsChangedSnapshotAndFalseVerification(t *testing.T) {
	stateRoot, journal := importJournalFixture(t)
	if err := os.WriteFile(filepath.Join(stateRoot, "imports", journal.ID, "project.txt"), []byte("changed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := createImportJournal(stateRoot, journal); err == nil {
		t.Fatal("changed source snapshot was bound")
	}
	if err := os.WriteFile(filepath.Join(stateRoot, "imports", journal.ID, "project.txt"), []byte("synthetic\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := createImportJournal(stateRoot, journal); err != nil {
		t.Fatal(err)
	}
	falseVerified := journal
	falseVerified.Phase = "verified"
	if err := advanceImportJournal(context.Background(), stateRoot, journal, falseVerified); err == nil {
		t.Fatal("unimplemented verification phase was accepted")
	}
	falseVerified.Phase = ImportTransferring
	falseVerified.SourceDigest = "0000000000000000000000000000000000000000000000000000000000000000"
	if err := advanceImportJournal(context.Background(), stateRoot, journal, falseVerified); err == nil {
		t.Fatal("source binding changed across transition")
	}
}
