package workspacex

import (
	"context"
	"fmt"

	"github.com/weshofmann/boxwarden/internal/backend"
	"github.com/weshofmann/boxwarden/internal/domain"
	"github.com/weshofmann/boxwarden/internal/lock"
	"github.com/weshofmann/boxwarden/internal/session"
)

// Attach binds an unattached volume to one stopped sandbox. It changes only
// metadata; launch admission remains closed until qualified formatting exists.
func Attach(ctx context.Context, stateRoot string, domainID domain.ID, volumeID, sessionName, mountPath string, observer backend.Observer) (Record, error) {
	if !validUUID(volumeID) || !validSessionName(sessionName) || !validMountPath(mountPath) {
		return Record{}, fmt.Errorf("invalid attach request")
	}
	sessionLock, err := lock.AcquireSession(ctx, stateRoot, string(domainID), sessionName)
	if err != nil {
		return Record{}, err
	}
	defer sessionLock.Release()
	storageLock, err := AcquireStorageOperation(ctx, stateRoot, domainID)
	if err != nil {
		return Record{}, err
	}
	defer storageLock.Release()
	record, err := LoadRecord(stateRoot, domainID, volumeID)
	if err != nil {
		return Record{}, err
	}
	if record.State != StateAvailable || record.Attachment != nil || record.Use != nil || record.Pending != nil {
		return Record{}, fmt.Errorf("workspace is not unattached and available")
	}
	target, err := stoppedSession(ctx, stateRoot, domainID, sessionName, observer)
	if err != nil {
		return Record{}, err
	}
	attached, err := listSessionAttachments(ctx, stateRoot, domainID, target.ID, sessionName)
	if err != nil {
		return Record{}, fmt.Errorf("inspect session workspace attachments: %w", err)
	}
	if len(attached) >= maxSessionAttachments {
		return Record{}, fmt.Errorf("session already has %d workspace attachments", maxSessionAttachments)
	}
	for _, prior := range attached {
		if prior.FilesystemUUID == record.FilesystemUUID {
			return Record{}, fmt.Errorf("session already attaches this filesystem UUID")
		}
	}
	record.Attachment = &Attachment{SessionID: target.ID, SessionName: sessionName, MountPath: mountPath}
	if err := saveRecordTransition(stateRoot, domainID, record, mutationAttach, nil); err != nil {
		return Record{}, err
	}
	return record, nil
}

// Detach clears only the attachment of a stopped sandbox. A crash between
// Detach and a subsequent Attach leaves the volume durably unattached.
func Detach(ctx context.Context, stateRoot string, domainID domain.ID, volumeID, sessionName string, observer backend.Observer) (Record, error) {
	if !validUUID(volumeID) || !validSessionName(sessionName) {
		return Record{}, fmt.Errorf("invalid detach request")
	}
	sessionLock, err := lock.AcquireSession(ctx, stateRoot, string(domainID), sessionName)
	if err != nil {
		return Record{}, err
	}
	defer sessionLock.Release()
	storageLock, err := AcquireStorageOperation(ctx, stateRoot, domainID)
	if err != nil {
		return Record{}, err
	}
	defer storageLock.Release()
	record, err := LoadRecord(stateRoot, domainID, volumeID)
	if err != nil {
		return Record{}, err
	}
	if record.State != StateAvailable || record.Attachment == nil || record.Attachment.SessionName != sessionName || record.Use != nil || record.Pending != nil {
		return Record{}, fmt.Errorf("workspace is not safely detachable")
	}
	target, err := stoppedSession(ctx, stateRoot, domainID, sessionName, observer)
	if err != nil {
		return Record{}, err
	}
	if target.ID != record.Attachment.SessionID {
		return Record{}, fmt.Errorf("session identity changed before detach")
	}
	record.Attachment = nil
	if err := saveRecordTransition(stateRoot, domainID, record, mutationDetach, nil); err != nil {
		return Record{}, err
	}
	return record, nil
}

// ReserveUse persists one exact backend and generation before a launch can
// attach an available workspace. Its stopped observation is a prelaunch gate;
// the later supervisor must hold the volume-use lock through stop/wait/reap.
// Disk launch admission remains closed until qualified formatting exists.
func ReserveUse(ctx context.Context, stateRoot string, domainID domain.ID, volumeID string, requested Use, observer backend.Observer) (Record, error) {
	if !validUUID(volumeID) || requested.BackendKind != "tart" || !validObjectID(requested.BackendObject) || !validUUID(requested.Generation) {
		return Record{}, fmt.Errorf("invalid workspace use request")
	}
	initial, err := LoadRecord(stateRoot, domainID, volumeID)
	if err != nil {
		return Record{}, err
	}
	if initial.Attachment == nil {
		return Record{}, fmt.Errorf("workspace is not attached")
	}
	sessionLock, err := lock.AcquireSession(ctx, stateRoot, string(domainID), initial.Attachment.SessionName)
	if err != nil {
		return Record{}, err
	}
	defer sessionLock.Release()
	storageLock, err := AcquireStorageOperation(ctx, stateRoot, domainID)
	if err != nil {
		return Record{}, err
	}
	defer storageLock.Release()
	record, err := LoadRecord(stateRoot, domainID, volumeID)
	if err != nil {
		return Record{}, err
	}
	if record.State != StateAvailable || record.Attachment == nil || *record.Attachment != *initial.Attachment || record.Use != nil || record.Pending != nil {
		return Record{}, fmt.Errorf("workspace attachment is not available for exact use")
	}
	target, err := stoppedSession(ctx, stateRoot, domainID, record.Attachment.SessionName, observer)
	if err != nil {
		return Record{}, err
	}
	if target.ID != record.Attachment.SessionID || target.Backend.ObjectID != requested.BackendObject {
		return Record{}, fmt.Errorf("workspace use does not match stopped sandbox identity")
	}
	record.Use = &requested
	if err := saveRecordTransition(stateRoot, domainID, record, mutationReserveUse, nil); err != nil {
		return Record{}, err
	}
	return record, nil
}

// ReleaseUseAfterObservedStop clears one exact reservation only after a fresh
// stopped observation of its exact backend object. The caller must retain its
// volume-use lock until the supervisor's stop/wait/reap path is complete.
// This transition acquires that lock before session and storage, so it cannot
// clear a child's reservation between disk admission and Tart spawn.
func ReleaseUseAfterObservedStop(ctx context.Context, stateRoot string, domainID domain.ID, volumeID string, expected Use, observer backend.Observer) (Record, error) {
	if !validUUID(volumeID) {
		return Record{}, fmt.Errorf("invalid volume ID")
	}
	initial, err := LoadRecord(stateRoot, domainID, volumeID)
	if err != nil {
		return Record{}, err
	}
	if initial.Attachment == nil {
		return Record{}, fmt.Errorf("workspace is not attached")
	}
	volumeLock, err := AcquireVolumeUse(ctx, stateRoot, domainID, volumeID)
	if err != nil {
		return Record{}, err
	}
	defer volumeLock.Release()
	sessionLock, err := lock.AcquireSession(ctx, stateRoot, string(domainID), initial.Attachment.SessionName)
	if err != nil {
		return Record{}, err
	}
	defer sessionLock.Release()
	storageLock, err := AcquireStorageOperation(ctx, stateRoot, domainID)
	if err != nil {
		return Record{}, err
	}
	defer storageLock.Release()
	record, err := LoadRecord(stateRoot, domainID, volumeID)
	if err != nil {
		return Record{}, err
	}
	if record.Attachment == nil || record.Attachment.SessionName != initial.Attachment.SessionName || record.Attachment.SessionID != initial.Attachment.SessionID || record.Use == nil || *record.Use != expected || record.Pending != nil {
		return Record{}, fmt.Errorf("workspace use reservation changed")
	}
	if expected.BackendKind != "tart" {
		return Record{}, fmt.Errorf("unsupported backend use")
	}
	sessionRecord, err := session.LoadRecord(stateRoot, string(domainID), record.Attachment.SessionName)
	if err != nil {
		return Record{}, fmt.Errorf("reload workspace-owning session: %w", err)
	}
	if sessionRecord.ID != record.Attachment.SessionID || sessionRecord.Backend.Kind != expected.BackendKind || sessionRecord.Backend.ObjectID != expected.BackendObject ||
		(sessionRecord.IntendedState != session.StateStopped && sessionRecord.IntendedState != session.StateStopping) ||
		(sessionRecord.IntendedState == session.StateStopping && sessionRecord.StartGeneration != expected.Generation) {
		return Record{}, fmt.Errorf("session is not stopping or stopped for exact workspace use")
	}
	if err := observedStopped(ctx, observer, expected.BackendObject); err != nil {
		return Record{}, err
	}
	record.Use = nil
	if err := saveRecordTransition(stateRoot, domainID, record, mutationReleaseUse, nil); err != nil {
		return Record{}, err
	}
	return record, nil
}

func stoppedSession(ctx context.Context, stateRoot string, domainID domain.ID, name string, observer backend.Observer) (session.Record, error) {
	record, err := session.LoadRecord(stateRoot, string(domainID), name)
	if err != nil {
		return session.Record{}, err
	}
	if record.IntendedState != session.StateStopped || record.Backend.Kind != "tart" {
		return session.Record{}, fmt.Errorf("session is not durably stopped")
	}
	if err := observedStopped(ctx, observer, record.Backend.ObjectID); err != nil {
		return session.Record{}, err
	}
	return record, nil
}

func observedStopped(ctx context.Context, observer backend.Observer, objectID string) error {
	if observer == nil {
		return fmt.Errorf("backend observer is required")
	}
	observation, err := observer.Observe(ctx, objectID)
	if err != nil {
		return fmt.Errorf("observe exact backend object: %w", err)
	}
	if observation.ObjectID != objectID || !observation.Exists || observation.State != backend.ObjectStopped {
		return fmt.Errorf("backend object %q is not observed stopped", objectID)
	}
	return nil
}
