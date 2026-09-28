package workspacex

import (
	"context"
	"errors"
	"fmt"
	"reflect"

	"github.com/weshofmann/boxwarden/internal/backend"
	"github.com/weshofmann/boxwarden/internal/domain"
	"github.com/weshofmann/boxwarden/internal/hostidentity"
	"github.com/weshofmann/boxwarden/internal/lock"
	"github.com/weshofmann/boxwarden/internal/workspaceformat"
)

// ReconcileLegacy binds a verified v1 formatter receipt to the independently
// enrolled APFS identity after proving the workspace is idle and any attached
// backend is durably and freshly stopped. It leaves the historical record and
// formatter receipt unchanged.
func ReconcileLegacy(ctx context.Context, stateRoot string, domainID domain.ID, volumeID string,
	storage hostidentity.StorageExpectation, observer backend.Observer) (Record, error) {
	return reconcileLegacy(ctx, stateRoot, domainID, volumeID, storage, observer,
		hostidentity.CheckStorage, workspaceformat.BindLegacy)
}

func reconcileLegacy(ctx context.Context, stateRoot string, domainID domain.ID, volumeID string,
	storage hostidentity.StorageExpectation, observer backend.Observer,
	check func(hostidentity.StorageExpectation) error,
	bind func(string, workspaceformat.Request, hostidentity.StorageExpectation) error) (result Record, err error) {
	if !validUUID(volumeID) || storage.StateRoot != stateRoot || check == nil || bind == nil {
		return Record{}, fmt.Errorf("invalid exact legacy reconciliation request")
	}
	if err := storage.Validate(); err != nil {
		return Record{}, err
	}
	// This guard is before every lock acquisition, because lock creation under
	// an absent removable mountpoint would write to the host filesystem.
	if err := check(storage); err != nil {
		return Record{}, fmt.Errorf("admit workspace backing storage: %w", err)
	}
	initial, err := LoadRecord(stateRoot, domainID, volumeID)
	if err != nil {
		return Record{}, err
	}
	volumeLock, err := AcquireVolumeUse(ctx, stateRoot, domainID, volumeID)
	if err != nil {
		return Record{}, err
	}
	defer func() { err = errors.Join(err, volumeLock.Release()) }()
	var sessionLock *lock.Held
	if initial.Attachment != nil {
		sessionLock, err = lock.AcquireSession(ctx, stateRoot, string(domainID), initial.Attachment.SessionName)
		if err != nil {
			return Record{}, err
		}
		defer func() { err = errors.Join(err, sessionLock.Release()) }()
	}
	storageLock, err := AcquireStorageOperation(ctx, stateRoot, domainID)
	if err != nil {
		return Record{}, err
	}
	defer func() { err = errors.Join(err, storageLock.Release()) }()
	if err := check(storage); err != nil {
		return Record{}, fmt.Errorf("recheck workspace backing storage under locks: %w", err)
	}
	record, err := LoadRecord(stateRoot, domainID, volumeID)
	if err != nil {
		return Record{}, err
	}
	if !reflect.DeepEqual(initial, record) || record.State != StateAvailable || record.Disk == nil || record.Use != nil || record.Pending != nil {
		return Record{}, fmt.Errorf("workspace is changed or has unresolved use or operation")
	}
	if record.Attachment != nil {
		owner, err := stoppedSession(ctx, stateRoot, domainID, record.Attachment.SessionName, observer)
		if err != nil {
			return Record{}, fmt.Errorf("prove attached backend stopped: %w", err)
		}
		if owner.ID != record.Attachment.SessionID {
			return Record{}, fmt.Errorf("workspace attachment differs from exact stopped session")
		}
	}
	request := workspaceformat.Request{Domain: domainID, VolumeID: volumeID,
		FilesystemUUID: record.FilesystemUUID, SizeBytes: record.SizeBytes}
	journal, err := workspaceformat.ReadJournal(stateRoot, request)
	if err != nil {
		return Record{}, fmt.Errorf("read exact legacy formatter receipt: %w", err)
	}
	if journal.Version != 1 || journal.State != workspaceformat.StateVerified || journal.Identity == nil ||
		journal.Domain != record.Domain || journal.VolumeID != record.VolumeID || journal.FilesystemUUID != record.FilesystemUUID ||
		journal.SizeBytes != record.SizeBytes || journal.Identity.Device != record.Disk.Device || journal.Identity.Inode != record.Disk.Inode {
		return Record{}, fmt.Errorf("available workspace record differs from exact verified v1 receipt")
	}
	if err := bind(stateRoot, request, storage); err != nil {
		return Record{}, fmt.Errorf("bind legacy workspace proof: %w", err)
	}
	return record, nil
}
