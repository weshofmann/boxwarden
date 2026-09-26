package workspacex

import (
	"context"
	"fmt"

	"github.com/weshofmann/boxwarden/internal/domain"
	"github.com/weshofmann/boxwarden/internal/workspaceformat"
)

// PromoteVerified makes a creating volume attachable only after the trusted
// formatter's verified journal and exact raw file pass a fresh admission.
// A failed record write leaves the journal verified and promotion retryable.
func PromoteVerified(ctx context.Context, stateRoot string, domainID domain.ID, volumeID string) (Record, error) {
	return promoteVerified(ctx, stateRoot, domainID, volumeID, nil, nil)
}

func promoteVerified(ctx context.Context, stateRoot string, domainID domain.ID, volumeID string, hook func(storeStage) error, syncAvailable func(string) error) (Record, error) {
	if !validUUID(volumeID) {
		return Record{}, fmt.Errorf("invalid volume ID")
	}
	held, err := AcquireStorageOperation(ctx, stateRoot, domainID)
	if err != nil {
		return Record{}, err
	}
	defer held.Release()
	record, err := LoadRecord(stateRoot, domainID, volumeID)
	if err != nil {
		return Record{}, err
	}
	if (record.State != StateCreating && record.State != StateAvailable) || record.Attachment != nil || record.Use != nil || record.Pending != nil {
		return Record{}, fmt.Errorf("workspace is not an unbound creatable volume")
	}
	request := workspaceformat.Request{Domain: domainID, VolumeID: volumeID, FilesystemUUID: record.FilesystemUUID, SizeBytes: record.SizeBytes}
	file, qualified, err := workspaceformat.Admit(stateRoot, request)
	if err != nil {
		return Record{}, fmt.Errorf("admit verified formatter journal and disk: %w", err)
	}
	if err := file.Close(); err != nil {
		return Record{}, fmt.Errorf("close admitted workspace disk: %w", err)
	}
	if qualified.Domain != record.Domain || qualified.VolumeID != volumeID || qualified.FilesystemUUID != record.FilesystemUUID || qualified.SizeBytes != record.SizeBytes || qualified.Identity.Device == 0 || qualified.Identity.Inode == 0 {
		return Record{}, fmt.Errorf("formatter qualification does not match workspace record")
	}
	verifiedDisk := DiskIdentity{Device: qualified.Identity.Device, Inode: qualified.Identity.Inode}
	if record.State == StateAvailable {
		if record.Disk == nil || *record.Disk != verifiedDisk {
			return Record{}, fmt.Errorf("available workspace disk differs from formatter qualification")
		}
		if syncAvailable == nil {
			syncAvailable = syncWorkspaceRecords
		}
		if err := syncAvailable(stateRoot); err != nil {
			return Record{}, fmt.Errorf("sync previously published workspace record: %w", err)
		}
		return record, nil
	}
	if record.Disk != nil {
		return Record{}, fmt.Errorf("creating workspace already claims a disk")
	}
	record.State = StateAvailable
	record.Disk = &verifiedDisk
	if err := saveRecordTransition(stateRoot, domainID, record, mutationPromoteVerified, hook); err != nil {
		return Record{}, fmt.Errorf("persist verified workspace: %w", err)
	}
	return record, nil
}

func syncWorkspaceRecords(stateRoot string) error {
	root, err := openStateRoot(stateRoot)
	if err != nil {
		return err
	}
	defer root.Close()
	workspaces, err := openChild(root, "workspaces", false)
	if err != nil {
		return err
	}
	defer workspaces.Close()
	return syncDirectory(workspaces)
}
