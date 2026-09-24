package workspacex

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/weshofmann/boxwarden/internal/domain"
)

func TestPrepareExportInspectorRequestBindsAdmittedSnapshotAndSelection(t *testing.T) {
	root := privateRoot(t)
	journal := testExportJournal(t.TempDir())
	if err := createExportJournal(root, journal); err != nil {
		t.Fatal(err)
	}
	if _, err := PrepareExportInspectorRequest(context.Background(), root, domain.ID("work"), journal.ID); err == nil {
		t.Fatal("copying journal produced a guest request")
	}
	ready := journal
	ready.Phase = ExportSnapshotReady
	ready.Snapshot = &ExportSnapshot{Identity: DiskIdentity{Device: 3, Inode: 5}, SHA256: strings.Repeat("a", 64)}
	if err := advanceExportJournal(context.Background(), root, journal, ready); err != nil {
		t.Fatal(err)
	}
	if _, err := PrepareExportInspectorRequest(context.Background(), root, domain.ID("personal"), journal.ID); err == nil {
		t.Fatal("cross-domain request admitted")
	}
	if _, err := PrepareExportInspectorRequest(context.Background(), root, domain.ID("work"), journal.ID); err == nil {
		t.Fatal("journal with absent snapshot produced a request")
	}
	// A valid private snapshot with exact recorded identity and digest is the
	// only state allowed to produce a guest request.
	snapshotDir := filepath.Join(root, "exports", journal.ID)
	if err := os.Mkdir(snapshotDir, 0o700); err != nil {
		t.Fatal(err)
	}
	snapshotPath := filepath.Join(snapshotDir, "snapshot.raw")
	if err := os.WriteFile(snapshotPath, []byte("known snapshot bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	// This deliberately does not try to satisfy the 64 MiB journal from a
	// short file; a separate encoder test covers the exact request envelope.
	if _, err := PrepareExportInspectorRequest(context.Background(), root, domain.ID("work"), journal.ID); err == nil {
		t.Fatal("short snapshot produced a guest request")
	}

	validRoot := privateRoot(t)
	valid := testExportJournal(t.TempDir())
	valid.SizeBytes = 4096
	if err := createExportJournal(validRoot, valid); err != nil {
		t.Fatal(err)
	}
	validDir := filepath.Join(validRoot, "exports", valid.ID)
	if err := os.Mkdir(validDir, 0o700); err != nil {
		t.Fatal(err)
	}
	validPath := filepath.Join(validDir, "snapshot.raw")
	content := make([]byte, valid.SizeBytes)
	copy(content, "bound export snapshot")
	if err := os.WriteFile(validPath, content, 0o600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(validPath)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := diskIdentity(info)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(content)
	validReady := valid
	validReady.Phase = ExportSnapshotReady
	validReady.Snapshot = &ExportSnapshot{Identity: identity, SHA256: hex.EncodeToString(digest[:])}
	if err := advanceExportJournal(context.Background(), validRoot, valid, validReady); err != nil {
		t.Fatal(err)
	}
	prepared, err := PrepareExportInspectorRequest(context.Background(), validRoot, domain.ID("work"), valid.ID)
	if err != nil || prepared.SnapshotPath != validPath || len(prepared.Request) == 0 {
		t.Fatalf("admitted snapshot request = %+v, %v", prepared, err)
	}
	content[0] ^= 0xff
	if err := os.WriteFile(validPath, content, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := PrepareExportInspectorRequest(context.Background(), validRoot, domain.ID("work"), valid.ID); err == nil {
		t.Fatal("mutated snapshot produced a request")
	}
}

func TestEncodeExportInspectorRequestMatchesGuestEnvelope(t *testing.T) {
	journal := testExportJournal(t.TempDir())
	journal.Phase = ExportSnapshotReady
	journal.Snapshot = &ExportSnapshot{Identity: DiskIdentity{Device: 3, Inode: 5}, SHA256: strings.Repeat("a", 64)}
	journal.Selected = []string{"project/report.txt", "notes"}
	raw, err := encodeExportInspectorRequest(journal)
	if err != nil {
		t.Fatal(err)
	}
	var request struct {
		Version        int      `json:"version"`
		Transaction    string   `json:"transaction"`
		FilesystemUUID string   `json:"filesystem_uuid"`
		DiskBytes      int64    `json:"disk_bytes"`
		Selected       []string `json:"selected"`
	}
	if err := json.Unmarshal(raw, &request); err != nil || request.Version != 1 ||
		request.Transaction != strings.ReplaceAll(journal.ID, "-", "") ||
		request.FilesystemUUID != journal.FilesystemUUID || request.DiskBytes != journal.SizeBytes ||
		len(request.Selected) != 2 || request.Selected[0] != "project/report.txt" || request.Selected[1] != "notes" {
		t.Fatalf("guest request = %+v, %v", request, err)
	}
	journal.Phase = ExportCopying
	journal.Snapshot = nil
	if _, err := encodeExportInspectorRequest(journal); err == nil {
		t.Fatal("non-ready journal encoded")
	}
}
