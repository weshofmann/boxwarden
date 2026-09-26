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

// ReleaseSessionUses clears an exact batch after the caller has proved the
// supervisor's stop/wait/reap path, or for Stopped intent that never admitted
// a launch. The fresh backend observation and volume locks also prevent a
// detached child from racing release. A partial release is retryable because
// already-cleared records are accepted only within the same exact attachment
// set and session state. The caller holds the outer transition lock only.
func ReleaseSessionUses(ctx context.Context, stateRoot string, domainID domain.ID, expected session.Record, observer backend.Observer) (err error) {
	return releaseSessionUses(ctx, stateRoot, domainID, expected, observer, func(record Record) error {
		return saveRecordTransition(stateRoot, domainID, record, mutationReleaseUse, nil)
	})
}

func releaseSessionUses(ctx context.Context, stateRoot string, domainID domain.ID, expected session.Record, observer backend.Observer, writeRelease func(Record) error) (err error) {
	if expected.Domain != domainID || (expected.IntendedState != session.StateStopping && expected.IntendedState != session.StateStopped) ||
		expected.Backend.Kind != "tart" || !validObjectID(expected.Backend.ObjectID) || !validUUID(expected.ID) || !validSessionName(string(expected.Name)) ||
		(expected.IntendedState == session.StateStopping && !validUUID(expected.StartGeneration)) ||
		(expected.IntendedState == session.StateStopped && expected.StartGeneration != "") {
		return fmt.Errorf("invalid stopped or stopping session use binding")
	}
	initial, err := ListSessionAttachments(ctx, stateRoot, domainID, expected.ID, string(expected.Name))
	if err != nil {
		return err
	}
	volumeLocks := make([]*lock.Held, len(initial))
	defer func() {
		for _, held := range volumeLocks {
			err = errors.Join(err, held.Release())
		}
	}()
	for i, record := range initial {
		volumeLocks[i], err = AcquireVolumeUse(ctx, stateRoot, domainID, record.VolumeID)
		if err != nil {
			return fmt.Errorf("lock workspace %q for use release: %w", record.VolumeID, err)
		}
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
	if err != nil {
		return fmt.Errorf("reload session before use release: %w", err)
	}
	if current != expected {
		return fmt.Errorf("session changed before workspace use release")
	}
	attached, err := listSessionAttachments(ctx, stateRoot, domainID, expected.ID, string(expected.Name))
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(initial, attached) {
		return fmt.Errorf("workspace attachment set changed before use release")
	}
	if err := observedStopped(ctx, observer, expected.Backend.ObjectID); err != nil {
		return fmt.Errorf("prove exact backend stopped before use release: %w", err)
	}
	for _, record := range attached {
		if record.State != StateAvailable || record.Pending != nil || record.Disk == nil {
			return fmt.Errorf("workspace %q is not releasable", record.VolumeID)
		}
		if record.Use == nil {
			continue
		}
		if record.Use.BackendKind != expected.Backend.Kind || record.Use.BackendObject != expected.Backend.ObjectID ||
			(expected.IntendedState == session.StateStopping && record.Use.Generation != expected.StartGeneration) {
			return fmt.Errorf("workspace %q has foreign or inexact use", record.VolumeID)
		}
	}
	for _, record := range attached {
		if record.Use == nil {
			continue
		}
		record.Use = nil
		if err := writeRelease(record); err != nil {
			return fmt.Errorf("persist workspace %q use release: %w", record.VolumeID, err)
		}
	}
	return nil
}
