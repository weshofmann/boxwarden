package workspacex

import (
	"context"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/weshofmann/boxwarden/internal/domain"
	"github.com/weshofmann/boxwarden/internal/workspaceformat"
)

type formatFixture func(context.Context, workspaceformat.FormatRequest) (workspaceformat.FormatEvidence, error)

func (f formatFixture) FormatAndVerify(ctx context.Context, request workspaceformat.FormatRequest) (workspaceformat.FormatEvidence, error) {
	return f(ctx, request)
}

// The fixture fills only the host-readable fields of a clean ext4 header.
// Real formatter qualification still requires a fresh Linux guest and e2fsck.
func writeFixtureExt4Header(path, uuid string) error {
	file, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	defer file.Close()
	if _, err := file.WriteAt([]byte{0x53, 0xef}, 1024+0x38); err != nil {
		return err
	}
	if _, err := file.WriteAt([]byte{0x01, 0x00}, 1024+0x3a); err != nil {
		return err
	}
	if _, err := file.WriteAt([]byte{0x04, 0x00, 0x00, 0x00}, 1024+0x5c); err != nil {
		return err
	}
	if _, err := file.WriteAt([]byte{0x40, 0x00, 0x00, 0x00}, 1024+0x60); err != nil {
		return err
	}
	raw, err := hex.DecodeString(strings.ReplaceAll(uuid, "-", ""))
	if err != nil {
		return err
	}
	if _, err := file.WriteAt(raw, 1024+0x68); err != nil {
		return err
	}
	return file.Sync()
}

func TestPromoteVerifiedRequiresExactFormatterJournalAndDisk(t *testing.T) {
	root := privateRoot(t)
	record := fixtureRecord()
	record.SizeBytes = 16 << 20
	if err := SaveRecord(root, domain.ID("work"), record); err != nil {
		t.Fatal(err)
	}
	if _, err := PromoteVerified(context.Background(), root, domain.ID("work"), record.VolumeID); err == nil {
		t.Fatal("unformatted record was promoted")
	}
	request := workspaceformat.Request{Domain: record.Domain, VolumeID: record.VolumeID, FilesystemUUID: record.FilesystemUUID, SizeBytes: record.SizeBytes}
	qualified, err := workspaceformat.Create(context.Background(), root, request, formatFixture(func(_ context.Context, request workspaceformat.FormatRequest) (workspaceformat.FormatEvidence, error) {
		if err := writeFixtureExt4Header(request.DiskPath, request.FilesystemUUID); err != nil {
			return workspaceformat.FormatEvidence{}, err
		}
		return workspaceformat.FormatEvidence{ObservedUUID: request.FilesystemUUID, WholeDevice: true, FilesystemClean: true}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	diskPath := filepath.Join(root, "volumes", record.VolumeID+".raw")
	heldPath := diskPath + ".held"
	if err := os.Rename(diskPath, heldPath); err != nil {
		t.Fatal(err)
	}
	replacement, err := os.OpenFile(diskPath, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := replacement.Truncate(record.SizeBytes); err != nil {
		t.Fatal(err)
	}
	if err := replacement.Close(); err != nil {
		t.Fatal(err)
	}
	if err := writeFixtureExt4Header(diskPath, record.FilesystemUUID); err != nil {
		t.Fatal(err)
	}
	if _, err := PromoteVerified(context.Background(), root, domain.ID("work"), record.VolumeID); err == nil {
		t.Fatal("replacement disk with matching header was promoted")
	}
	if err := os.Remove(diskPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(heldPath, diskPath); err != nil {
		t.Fatal(err)
	}
	if _, err := promoteVerified(context.Background(), root, domain.ID("work"), record.VolumeID, func(stage storeStage) error {
		if stage == storeAfterRename {
			return errors.New("injected post-rename failure")
		}
		return nil
	}, nil); err == nil {
		t.Fatal("injected post-rename failure was not reported")
	}
	syncAttempted := false
	if _, err := promoteVerified(context.Background(), root, domain.ID("work"), record.VolumeID, nil, func(string) error {
		syncAttempted = true
		return errors.New("injected directory sync failure")
	}); err == nil || !syncAttempted {
		t.Fatalf("ambiguous promotion retry did not require directory sync: attempted=%v err=%v", syncAttempted, err)
	}
	promoted, err := PromoteVerified(context.Background(), root, domain.ID("work"), record.VolumeID)
	if err != nil {
		t.Fatal(err)
	}
	if promoted.State != StateAvailable || promoted.Disk == nil || promoted.Disk.Device != qualified.Identity.Device || promoted.Disk.Inode != qualified.Identity.Inode {
		t.Fatalf("promotion lost exact disk identity: %#v", promoted)
	}
	if repeated, err := PromoteVerified(context.Background(), root, domain.ID("work"), record.VolumeID); err != nil || !reflect.DeepEqual(repeated, promoted) {
		t.Fatalf("exact promotion retry = %#v, %v", repeated, err)
	}
	loaded, err := LoadRecord(root, domain.ID("work"), record.VolumeID)
	if err != nil || loaded.State != StateAvailable {
		t.Fatalf("failed to retain promoted metadata: %#v, %v", loaded, err)
	}
}
