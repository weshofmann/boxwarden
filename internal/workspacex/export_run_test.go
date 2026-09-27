package workspacex

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/weshofmann/boxwarden/internal/backend"
	"github.com/weshofmann/boxwarden/internal/domain"
	"github.com/weshofmann/boxwarden/internal/exportx"
	"github.com/weshofmann/boxwarden/internal/workspaceformat"
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
		path, err := publishCapturedExport(ctx, root, "work", j.ID, captured, allowSyntheticExportHeadroom, syntheticExportReceiverReserve)
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

func TestPublicResumeAbortsInterruptedCopyAndReleasesWorkspace(t *testing.T) {
	root, stopped := stoppedLaunchFixture(t)
	parent := t.TempDir()
	if err := os.Chmod(parent, 0700); err != nil {
		t.Fatal(err)
	}
	observer := stoppedObserver{state: backend.ObjectStopped, object: stopped.Backend.ObjectID}
	returned, err := createExportSnapshot(t.Context(), root, "work", testVolumeID, parent, []string{"project/report.txt"}, observer,
		func(_ context.Context, _ string, _ workspaceformat.Request, _ *os.File, dir *os.Root, _ ExportJournal) (ExportSnapshot, error) {
			f, e := dir.OpenFile("snapshot.raw", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
			if e != nil {
				return ExportSnapshot{}, e
			}
			_, e = f.Write([]byte("partial"))
			e = errors.Join(e, f.Close())
			if e != nil {
				return ExportSnapshot{}, e
			}
			return ExportSnapshot{}, errors.New("injected copy interruption")
		}, allowSyntheticExportHeadroom)
	if err == nil || returned.ID == "" {
		t.Fatalf("missing interrupted transaction: %+v %v", returned, err)
	}
	for _, unsafeObserver := range []backend.Observer{nil, stoppedObserver{state: backend.ObjectRunning, object: stopped.Backend.ObjectID}, stoppedObserver{state: backend.ObjectStopped, object: "foreign-backend"}} {
		if _, _, err := ResumeSelectedWorkspace(t.Context(), root, "work", returned.ID, "/unused/source", "/unused/iso", "/unused/go", unsafeObserver); err == nil {
			t.Fatal("uncertain or foreign backend cleared interrupted copy")
		}
		volume, err := LoadRecord(root, "work", testVolumeID)
		if err != nil || volume.Pending == nil || volume.Pending.ID != returned.ID {
			t.Fatalf("refusal changed Pending: %+v %v", volume, err)
		}
		if _, err := os.Stat(filepath.Join(root, returned.SnapshotPath)); err != nil {
			t.Fatalf("refusal removed partial copy: %v", err)
		}
	}
	result, path, err := ResumeSelectedWorkspace(t.Context(), root, "work", returned.ID, "/unused/source", "/unused/iso", "/unused/go", observer)
	if err != nil || result.Phase != ExportAborted || path != "" {
		t.Fatalf("public copy recovery: %+v %q %v", result, path, err)
	}
	volume, err := LoadRecord(root, "work", testVolumeID)
	if err != nil || volume.Pending != nil || volume.Use != nil {
		t.Fatalf("recovery stranded workspace: %+v %v", volume, err)
	}
	if _, err := os.Stat(filepath.Join(root, returned.SnapshotPath)); !os.IsNotExist(err) {
		t.Fatalf("partial snapshot survived: %v", err)
	}
	if _, err := PrepareSessionStart(t.Context(), root, "work", stopped, testGeneration, observer); err != nil {
		t.Fatalf("recovered workspace cannot start: %v", err)
	}
}

func TestReadySnapshotResumeDoesNotWaitForLiveVolumeLease(t *testing.T) {
	root, stopped := stoppedLaunchFixture(t)
	parent := t.TempDir()
	if err := os.Chmod(parent, 0700); err != nil {
		t.Fatal(err)
	}
	observer := stoppedObserver{state: backend.ObjectStopped, object: stopped.Backend.ObjectID}
	ready, err := createExportSnapshot(t.Context(), root, "work", testVolumeID, parent, []string{"project/report.txt"}, observer, copyExportSnapshot, allowSyntheticExportHeadroom)
	if err != nil {
		t.Fatal(err)
	}
	held, err := AcquireVolumeUse(t.Context(), root, "work", testVolumeID)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Release()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	// The nonexistent source root stops bundle preparation before a helper can
	// launch. Independent snapshot admission must reach that check while the
	// original workspace's live lease remains held.
	_, _, err = ResumeSelectedWorkspace(ctx, root, "work", ready.ID, filepath.Join(t.TempDir(), "missing-source"), "/unused/iso", "/unused/go", observer)
	if err == nil || errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("ready snapshot waited for live volume lease: %v", err)
	}
	record, err := LoadRecord(root, "work", testVolumeID)
	if err != nil || record.Pending != nil {
		t.Fatalf("snapshot resume changed volume: %+v %v", record, err)
	}
}
