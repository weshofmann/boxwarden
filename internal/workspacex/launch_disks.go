package workspacex

import (
	"context"
	"errors"
	"fmt"
	"reflect"

	"github.com/weshofmann/boxwarden/internal/backend"
	"github.com/weshofmann/boxwarden/internal/domain"
	"github.com/weshofmann/boxwarden/internal/lock"
	"github.com/weshofmann/boxwarden/internal/session"
	"github.com/weshofmann/boxwarden/internal/workspaceformat"
)

// AdmitLaunchDisks independently turns exact, durably reserved workspace Uses
// into one-use backend leases. It holds volume-use locks before session and
// storage locks, then releases the latter two before returning. The caller
// must close unclaimed leases or transfer them to the exact Tart handle.
func AdmitLaunchDisks(ctx context.Context, stateRoot string, domainID domain.ID, expected session.Record, observer backend.Observer) (set *backend.ManagedDiskSet, err error) {
	if expected.Domain != domainID || expected.IntendedState != session.StateStarting || expected.Backend.Kind != "tart" || !validUUID(expected.ID) || !validUUID(expected.StartGeneration) || !validSessionName(string(expected.Name)) {
		return nil, fmt.Errorf("invalid exact starting session binding")
	}
	initial, err := ListSessionAttachments(ctx, stateRoot, domainID, expected.ID, string(expected.Name))
	if err != nil {
		return nil, err
	}
	volumeLocks := make([]*lock.Held, len(initial))
	disks := make([]*backend.ManagedDisk, 0, len(initial))
	defer func() {
		if err != nil {
			for _, disk := range disks {
				err = errors.Join(err, disk.Close())
			}
			for _, held := range volumeLocks {
				err = errors.Join(err, held.Release())
			}
		}
	}()
	for i, record := range initial {
		volumeLocks[i], err = AcquireVolumeUse(ctx, stateRoot, domainID, record.VolumeID)
		if err != nil {
			return nil, fmt.Errorf("lock workspace %q for exact launch: %w", record.VolumeID, err)
		}
	}
	sessionLock, err := lock.AcquireSession(ctx, stateRoot, string(domainID), string(expected.Name))
	if err != nil {
		return nil, err
	}
	defer sessionLock.Release()
	storageLock, err := AcquireStorageOperation(ctx, stateRoot, domainID)
	if err != nil {
		return nil, err
	}
	defer storageLock.Release()
	current, err := session.LoadRecord(stateRoot, string(domainID), string(expected.Name))
	if err != nil {
		return nil, fmt.Errorf("reload starting session before workspace admission: %w", err)
	}
	if current != expected {
		return nil, fmt.Errorf("starting session changed before workspace admission")
	}
	attached, err := listSessionAttachments(ctx, stateRoot, domainID, expected.ID, string(expected.Name))
	if err != nil {
		return nil, fmt.Errorf("reload workspace attachments before launch: %w", err)
	}
	if !reflect.DeepEqual(initial, attached) {
		return nil, fmt.Errorf("workspace attachment set changed before launch admission")
	}
	if len(attached) > 0 {
		if err := observedStopped(ctx, observer, expected.Backend.ObjectID); err != nil {
			return nil, fmt.Errorf("prove exact backend stopped before workspace launch: %w", err)
		}
	}
	wantUse := Use{BackendKind: expected.Backend.Kind, BackendObject: expected.Backend.ObjectID, Generation: expected.StartGeneration}
	for i, record := range attached {
		if record.State != StateAvailable || record.Disk == nil || record.Pending != nil || record.Use == nil || *record.Use != wantUse {
			return nil, fmt.Errorf("workspace %q lacks exact starting-generation use", record.VolumeID)
		}
		request := workspaceformat.Request{Domain: domainID, VolumeID: record.VolumeID, FilesystemUUID: record.FilesystemUUID, SizeBytes: record.SizeBytes}
		file, qualified, admitErr := workspaceformat.Admit(stateRoot, request)
		if admitErr != nil {
			return nil, fmt.Errorf("admit workspace %q formatter proof: %w", record.VolumeID, admitErr)
		}
		if qualified.Domain != domainID || qualified.VolumeID != record.VolumeID || qualified.FilesystemUUID != record.FilesystemUUID || qualified.SizeBytes != record.SizeBytes || qualified.Identity.Device != record.Disk.Device || qualified.Identity.Inode != record.Disk.Inode {
			return nil, errors.Join(fmt.Errorf("workspace %q disk identity differs from formatter proof", record.VolumeID), file.Close())
		}
		disk, diskErr := backend.NewManagedDisk(stateRoot, domainID, record.VolumeID, expected.Backend.ObjectID, expected.StartGeneration, file, volumeLocks[i])
		if diskErr != nil {
			return nil, errors.Join(fmt.Errorf("pin workspace %q disk lease: %w", record.VolumeID, diskErr), file.Close())
		}
		volumeLocks[i] = nil // NewManagedDisk now owns the live lock and file.
		disks = append(disks, disk)
	}
	if len(disks) == 0 {
		if releaseErr := errors.Join(storageLock.Release(), sessionLock.Release()); releaseErr != nil {
			return nil, releaseErr
		}
		return nil, nil
	}
	set, err = backend.NewManagedDiskSet(disks...)
	if err != nil {
		return nil, err
	}
	if releaseErr := errors.Join(storageLock.Release(), sessionLock.Release()); releaseErr != nil {
		return nil, errors.Join(releaseErr, set.CloseUnclaimed())
	}
	return set, nil
}
