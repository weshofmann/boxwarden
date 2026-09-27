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
)

// WithStoppedRebuildGate holds the volume-use, session, and storage locks
// through reservation of a replacement system. The caller holds the outer
// session transition lock and may acquire the domain golden lock inside
// reserve. It must persist rebuild intent before returning; backend cloning
// follows after these locks are released, when the journal gates mutations.
func WithStoppedRebuildGate(ctx context.Context, stateRoot string, domainID domain.ID, expected session.Record, observer backend.Observer, reserve func() error) (err error) {
	if expected.Domain != domainID || expected.Version != 2 || expected.IntendedState != session.StateStopped ||
		expected.Backend.Kind != "tart" || !validUUID(expected.ID) || !validSessionName(string(expected.Name)) ||
		!validObjectID(expected.Backend.ObjectID) || expected.StartGeneration != "" || reserve == nil {
		return fmt.Errorf("invalid stopped rebuild reservation")
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
			return fmt.Errorf("lock workspace %q for rebuild: %w", volume.VolumeID, lockErr)
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
		return fmt.Errorf("session changed before rebuild reservation: %v", err)
	}
	if err := session.RequireNoRebuild(stateRoot, domainID, string(expected.Name)); err != nil {
		return err
	}
	attached, err := listSessionAttachments(ctx, stateRoot, domainID, expected.ID, string(expected.Name))
	if err != nil || !reflect.DeepEqual(initial, attached) {
		return fmt.Errorf("workspace attachment set changed before rebuild reservation: %v", err)
	}
	if err := observedStopped(ctx, observer, expected.Backend.ObjectID); err != nil {
		return fmt.Errorf("prove old system stopped for rebuild: %w", err)
	}
	for _, volume := range attached {
		if volume.State != StateAvailable || volume.Disk == nil || volume.Pending != nil || volume.Use != nil ||
			volume.Attachment == nil || volume.Attachment.SessionID != expected.ID || volume.Attachment.SessionName != string(expected.Name) {
			return fmt.Errorf("workspace %q is not idle for rebuild", volume.VolumeID)
		}
	}
	return reserve()
}
