package workspacex

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/weshofmann/boxwarden/internal/domain"
	"github.com/weshofmann/boxwarden/internal/exportx"
)

func TestResumeSelectedWorkspaceFromInspectedJournal(t *testing.T) {
	root := privateRoot(t)
	destination := filepath.Join(t.TempDir(), "returned")
	if err := os.Mkdir(destination, 0o700); err != nil {
		t.Fatal(err)
	}
	parentInfo, err := os.Lstat(destination)
	if err != nil {
		t.Fatal(err)
	}
	parentID, err := diskIdentity(parentInfo)
	if err != nil {
		t.Fatal(err)
	}
	j := testExportJournal(destination)
	j.SizeBytes = 4096
	j.Destination = parentID
	if err := createExportJournal(root, j); err != nil {
		t.Fatal(err)
	}
	snapshotDir := filepath.Join(root, "exports", j.ID)
	if err := os.Mkdir(snapshotDir, 0o700); err != nil {
		t.Fatal(err)
	}
	content := make([]byte, j.SizeBytes)
	copy(content, "exact inspected snapshot")
	snapshot := filepath.Join(snapshotDir, "snapshot.raw")
	if err := os.WriteFile(snapshot, content, 0o600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := diskIdentity(info)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(content)
	ready := j
	ready.Phase = ExportSnapshotReady
	ready.Snapshot = &ExportSnapshot{Identity: identity, SHA256: hex.EncodeToString(digest[:])}
	if err := advanceExportJournal(context.Background(), root, j, ready); err != nil {
		t.Fatal(err)
	}
	inspected := ready
	inspected.Phase = ExportInspected
	if err := advanceExportJournal(context.Background(), root, ready, inspected); err != nil {
		t.Fatal(err)
	}
	finishes := 0
	finish := func(ctx context.Context, gotRoot string, gotDomain domain.ID, got ExportJournal, sourceRoot, isoPath, goBinary string) (ExportJournal, string, error) {
		finishes++
		if gotRoot != root || gotDomain != "work" || !reflect.DeepEqual(got, inspected) || sourceRoot != "/clean/source" || isoPath != "/pinned/ubuntu.iso" || goBinary != "/pinned/go" {
			t.Fatalf("resume lost exact journal or inputs: %+v", got)
		}
		captured, err := exportx.CaptureExportInspector(ctx, os.Args[0], []string{
			"-test.run=^TestExportPublishHelperProcess$", "publish-helper", exportTransactionHex(j.ID), "valid",
		}, snapshotDir)
		if err != nil {
			return got, "", err
		}
		path, err := PublishCapturedExport(ctx, root, "work", j.ID, captured)
		if err != nil {
			return got, path, err
		}
		published := inspected
		published.Phase = ExportPublished
		return published, path, nil
	}
	final := filepath.Join(destination, exportTransactionHex(j.ID))
	if err := os.Mkdir(final, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, _, err := resumeSelectedWorkspace(context.Background(), root, "work", j.ID, "/clean/source", "/pinned/ubuntu.iso", "/pinned/go", finish); err == nil || finishes != 0 {
		t.Fatalf("existing final directory did not block inspector rerun: %v, finishes=%d", err, finishes)
	}
	if err := os.Remove(final); err != nil {
		t.Fatal(err)
	}
	content[0] ^= 0xff
	if err := os.WriteFile(snapshot, content, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := resumeSelectedWorkspace(context.Background(), root, "work", j.ID, "/clean/source", "/pinned/ubuntu.iso", "/pinned/go", finish); err == nil || finishes != 0 {
		t.Fatalf("changed snapshot reached inspector rerun: %v, finishes=%d", err, finishes)
	}
	content[0] ^= 0xff
	if err := os.WriteFile(snapshot, content, 0o600); err != nil {
		t.Fatal(err)
	}
	staleSpool := filepath.Join(snapshotDir, "stream.bin")
	if err := os.WriteFile(staleSpool, []byte("unverified stale output"), 0o600); err != nil {
		t.Fatal(err)
	}
	if result, path, err := resumeSelectedWorkspace(context.Background(), root, "work", j.ID, "/clean/source", "/pinned/ubuntu.iso", "/pinned/go", finish); err == nil || result.Phase != ExportInspected || path != "" || finishes != 0 {
		t.Fatalf("stale spool was adopted or deleted: %s, %q, %v, finishes=%d", result.Phase, path, err, finishes)
	}
	if body, err := os.ReadFile(staleSpool); err != nil || string(body) != "unverified stale output" {
		t.Fatalf("unverified spool changed: %q, %v", body, err)
	}
	if err := os.Remove(staleSpool); err != nil {
		t.Fatal(err)
	}
	result, path, err := resumeSelectedWorkspace(context.Background(), root, "work", j.ID, "/clean/source", "/pinned/ubuntu.iso", "/pinned/go", finish)
	if err != nil || finishes != 1 || result.Phase != ExportPublished || path != final {
		t.Fatalf("inspected resume = %s, %q, %v, finishes=%d", result.Phase, path, err, finishes)
	}
	if body, err := os.ReadFile(filepath.Join(final, "project", "report.txt")); err != nil || strings.TrimSpace(string(body)) != "hello" {
		t.Fatalf("resumed publication content = %q, %v", body, err)
	}
}
