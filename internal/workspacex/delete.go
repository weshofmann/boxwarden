package workspacex

import (
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"

	"github.com/weshofmann/boxwarden/internal/backend"
	"github.com/weshofmann/boxwarden/internal/domain"
	"github.com/weshofmann/boxwarden/internal/lock"
	"github.com/weshofmann/boxwarden/internal/session"
	"github.com/weshofmann/boxwarden/internal/workspaceformat"
)

// WithStoppedDeleteGate reserves deletion under the same lock order as a
// rebuild: attached volume-use locks, then session, then storage. Deletion
// cannot begin while an export, start, or workspace mutation owns a volume.
func WithStoppedDeleteGate(ctx context.Context, stateRoot string, domainID domain.ID, expected session.Record, observer backend.Observer, reserve func() error) error {
	return WithStoppedRebuildGate(ctx, stateRoot, domainID, expected, observer, func() error {
		attached, err := listSessionAttachments(ctx, stateRoot, domainID, expected.ID, string(expected.Name))
		if err != nil {
			return err
		}
		for _, volume := range attached {
			if err := admitRetainedVolume(stateRoot, domainID, volume); err != nil {
				return err
			}
		}
		return reserve()
	})
}

// FinalizeDeletedSession clears exact idle attachments only after the old VM
// is freshly observed absent. It never removes a disk or a volume record.
// The deleting record is removed last, under the same session/storage locks.
func FinalizeDeletedSession(ctx context.Context, stateRoot string, domainID domain.ID, expected session.Record, observer backend.Observer) (err error) {
	if expected.Domain != domainID || expected.Version != 2 || expected.IntendedState != session.StateDeleting ||
		expected.Backend.Kind != "tart" || !validUUID(expected.ID) || !validSessionName(string(expected.Name)) ||
		!validObjectID(expected.Backend.ObjectID) || expected.StartGeneration != "" || observer == nil {
		return fmt.Errorf("invalid deleted session finalization")
	}
	initial, err := ListSessionAttachments(ctx, stateRoot, domainID, expected.ID, string(expected.Name))
	if err != nil {
		return err
	}
	volumeLocks := make([]*lock.Held, 0, len(initial))
	defer func() {
		for i := len(volumeLocks) - 1; i >= 0; i-- {
			err = errors.Join(err, volumeLocks[i].Release())
		}
	}()
	for _, volume := range initial {
		held, lockErr := AcquireVolumeUse(ctx, stateRoot, domainID, volume.VolumeID)
		if lockErr != nil {
			return fmt.Errorf("lock workspace %q for deleted session: %w", volume.VolumeID, lockErr)
		}
		volumeLocks = append(volumeLocks, held)
	}
	sessionLock, err := lock.AcquireSession(ctx, stateRoot, string(domainID), string(expected.Name))
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, sessionLock.Release()) }()
	storageLock, err := AcquireStorageOperation(ctx, stateRoot, domainID)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, storageLock.Release()) }()
	current, err := session.LoadRecord(stateRoot, string(domainID), string(expected.Name))
	if err != nil || current != expected {
		return fmt.Errorf("deleting session changed before finalization: %v", err)
	}
	if err := session.RequireNoRebuild(stateRoot, domainID, string(expected.Name)); err != nil {
		return err
	}
	attached, err := listSessionAttachments(ctx, stateRoot, domainID, expected.ID, string(expected.Name))
	if err != nil || !reflect.DeepEqual(initial, attached) {
		return fmt.Errorf("workspace attachment set changed before deletion finalization: %v", err)
	}
	observation, err := observer.Observe(ctx, expected.Backend.ObjectID)
	if err != nil || observation.ObjectID != expected.Backend.ObjectID || observation.Exists || observation.State != backend.ObjectUnknown {
		return fmt.Errorf("deleted backend is not exactly absent: %v", err)
	}
	for _, volume := range attached {
		if volume.State != StateAvailable || volume.Disk == nil || volume.Pending != nil || volume.Use != nil ||
			volume.Attachment == nil || volume.Attachment.SessionID != expected.ID || volume.Attachment.SessionName != string(expected.Name) {
			return fmt.Errorf("workspace %q is not idle for deleted session", volume.VolumeID)
		}
		if err := admitRetainedVolume(stateRoot, domainID, volume); err != nil {
			return err
		}
	}
	for _, volume := range attached {
		volume.Attachment = nil
		if err := saveRecordTransition(stateRoot, domainID, volume, mutationDetach, nil); err != nil {
			return fmt.Errorf("detach retained workspace %q: %w", volume.VolumeID, err)
		}
	}
	remaining, err := listSessionAttachments(ctx, stateRoot, domainID, expected.ID, string(expected.Name))
	if err != nil || len(remaining) != 0 {
		return fmt.Errorf("workspace attachments remain after deletion: count %d, error %v", len(remaining), err)
	}
	// An earlier detach rename may have succeeded but its directory sync
	// failed. Sync the registry even when no attachment remains on this retry.
	if err := syncDeletedWorkspaceRegistry(stateRoot); err != nil {
		return fmt.Errorf("sync retained workspace registry: %w", err)
	}
	return session.RemoveDeletingRecordLocked(stateRoot, expected)
}

func admitRetainedVolume(stateRoot string, domainID domain.ID, volume Record) error {
	if volume.Disk == nil {
		return fmt.Errorf("workspace %q has no recorded disk identity", volume.VolumeID)
	}
	request := workspaceformat.Request{Domain: domainID, VolumeID: volume.VolumeID,
		FilesystemUUID: volume.FilesystemUUID, SizeBytes: volume.SizeBytes}
	file, qualified, err := workspaceformat.Admit(stateRoot, request)
	if err != nil {
		return fmt.Errorf("admit retained workspace %q: %w", volume.VolumeID, err)
	}
	if qualified.Domain != domainID || qualified.VolumeID != volume.VolumeID || qualified.FilesystemUUID != volume.FilesystemUUID ||
		qualified.SizeBytes != volume.SizeBytes || qualified.Identity.Device != volume.Disk.Device || qualified.Identity.Inode != volume.Disk.Inode {
		return errors.Join(fmt.Errorf("workspace %q disk differs from formatter proof", volume.VolumeID), file.Close())
	}
	return file.Close()
}

func syncDeletedWorkspaceRegistry(stateRoot string) error {
	root, err := openStateRoot(stateRoot)
	if err != nil {
		return err
	}
	defer root.Close()
	workspaces, err := openChild(root, "workspaces", false)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer workspaces.Close()
	return syncWorkspaceRecordDirectory(workspaces)
}
