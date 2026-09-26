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

// PrepareSessionStart reserves the complete attached workspace set before
// publishing Starting as the batch launch commit marker. A failed or
// ambiguous intermediate write leaves the session Stopped (or an exact
// Starting record) and its Uses fail-closed; it never authorizes launch on an
// error. Callers hold the outer session transition lock, not the session lock.
func PrepareSessionStart(ctx context.Context, stateRoot string, domainID domain.ID, expected session.Record, generation string, observer backend.Observer) (started session.Record, err error) {
	return prepareSessionStart(ctx, stateRoot, domainID, expected, generation, observer, func(record Record) error {
		return saveRecordTransition(stateRoot, domainID, record, mutationReserveUse, nil)
	})
}

// PrepareRebuildSessionStart permits only the exact journaled candidate after
// the stable session record has switched. It uses the same volume-first batch
// reservation and Starting commit marker as ordinary start.
func PrepareRebuildSessionStart(ctx context.Context, stateRoot string, domainID domain.ID, expected session.Record, generation string, observer backend.Observer, journal session.RebuildJournal) (session.Record, error) {
	return prepareSessionStartJournal(ctx, stateRoot, domainID, expected, generation, observer, &journal, func(record Record) error {
		return saveRecordTransition(stateRoot, domainID, record, mutationReserveUse, nil)
	})
}

func prepareSessionStart(ctx context.Context, stateRoot string, domainID domain.ID, expected session.Record, generation string, observer backend.Observer, writeUse func(Record) error) (started session.Record, err error) {
	return prepareSessionStartJournal(ctx, stateRoot, domainID, expected, generation, observer, nil, writeUse)
}

func prepareSessionStartJournal(ctx context.Context, stateRoot string, domainID domain.ID, expected session.Record, generation string, observer backend.Observer, rebuild *session.RebuildJournal, writeUse func(Record) error) (started session.Record, err error) {
	if expected.Domain != domainID || expected.Version != 2 || expected.IntendedState != session.StateStopped || expected.Backend.Kind != "tart" ||
		!validUUID(expected.ID) || !validSessionName(string(expected.Name)) || !validObjectID(expected.Backend.ObjectID) || !validUUID(generation) {
		return session.Record{}, fmt.Errorf("invalid stopped session or generation")
	}
	initial, err := ListSessionAttachments(ctx, stateRoot, domainID, expected.ID, string(expected.Name))
	if err != nil {
		return session.Record{}, err
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
			return session.Record{}, fmt.Errorf("lock workspace %q for start reservation: %w", record.VolumeID, err)
		}
	}
	sessionLock, err := lock.AcquireSession(ctx, stateRoot, string(domainID), string(expected.Name))
	if err != nil {
		return session.Record{}, err
	}
	defer func() { err = errors.Join(err, sessionLock.Release()) }()
	storageLock, err := AcquireStorageOperation(ctx, stateRoot, domainID)
	if err != nil {
		return session.Record{}, err
	}
	defer func() { err = errors.Join(err, storageLock.Release()) }()
	current, err := session.LoadRecord(stateRoot, string(domainID), string(expected.Name))
	if err != nil {
		return session.Record{}, fmt.Errorf("reload stopped session before workspace reservation: %w", err)
	}
	if current != expected {
		return session.Record{}, fmt.Errorf("stopped session changed before workspace reservation")
	}
	if rebuild == nil {
		if err := session.RequireNoRebuild(stateRoot, domainID, string(expected.Name)); err != nil {
			return session.Record{}, err
		}
	} else {
		currentJournal, loadErr := session.LoadRebuildJournal(stateRoot, domainID, string(expected.Name))
		if loadErr != nil || currentJournal != *rebuild || (currentJournal.Phase != session.RebuildCutover && currentJournal.Phase != session.RebuildReady && currentJournal.Phase != session.RebuildRetiring) ||
			currentJournal.SessionID != expected.ID || currentJournal.CandidateBackend != expected.Backend.ObjectID || currentJournal.CandidateRevision != expected.GoldenRevision {
			return session.Record{}, fmt.Errorf("workspace start lacks exact candidate rebuild journal: %v", loadErr)
		}
	}
	attached, err := listSessionAttachments(ctx, stateRoot, domainID, expected.ID, string(expected.Name))
	if err != nil {
		return session.Record{}, fmt.Errorf("reload workspace attachments before reservation: %w", err)
	}
	if !reflect.DeepEqual(initial, attached) {
		return session.Record{}, fmt.Errorf("workspace attachment set changed before reservation")
	}
	if err := observedStopped(ctx, observer, expected.Backend.ObjectID); err != nil {
		return session.Record{}, fmt.Errorf("prove exact backend stopped before reservation: %w", err)
	}
	use := Use{BackendKind: expected.Backend.Kind, BackendObject: expected.Backend.ObjectID, Generation: generation}
	for _, record := range attached {
		if record.State != StateAvailable || record.Disk == nil || record.Pending != nil || record.Use != nil {
			return session.Record{}, fmt.Errorf("workspace %q is not available for exact start", record.VolumeID)
		}
		request := workspaceformat.Request{Domain: domainID, VolumeID: record.VolumeID, FilesystemUUID: record.FilesystemUUID, SizeBytes: record.SizeBytes}
		file, qualified, admitErr := workspaceformat.Admit(stateRoot, request)
		if admitErr != nil {
			return session.Record{}, fmt.Errorf("admit workspace %q before reservation: %w", record.VolumeID, admitErr)
		}
		closeErr := file.Close()
		if closeErr != nil {
			return session.Record{}, closeErr
		}
		if qualified.Identity.Device != record.Disk.Device || qualified.Identity.Inode != record.Disk.Inode {
			return session.Record{}, fmt.Errorf("workspace %q formatter identity changed", record.VolumeID)
		}
	}
	for _, record := range attached {
		record.Use = &use
		if err := writeUse(record); err != nil {
			return session.Record{}, fmt.Errorf("persist workspace %q use before starting: %w", record.VolumeID, err)
		}
	}
	started = expected
	started.IntendedState = session.StateStarting
	started.StartGeneration = generation
	started.Readiness = session.ReadinessRecord{Status: session.ReadinessStarting}
	if err := session.SaveRecord(stateRoot, domainID, started); err != nil {
		return session.Record{}, fmt.Errorf("persist starting session after workspace uses: %w", err)
	}
	return started, nil
}

// VerifySessionUses checks the durable batch marker before an already
// Starting/Running generation can be retried or accepted as ready. A running
// child retains volume-use locks, so this read takes only the storage lock.
func VerifySessionUses(ctx context.Context, stateRoot string, domainID domain.ID, expected session.Record) error {
	if expected.Domain != domainID || (expected.IntendedState != session.StateStarting && expected.IntendedState != session.StateRunning) ||
		!validUUID(expected.ID) || !validUUID(expected.StartGeneration) || !validSessionName(string(expected.Name)) {
		return fmt.Errorf("invalid exact session use binding")
	}
	storageLock, err := AcquireStorageOperation(ctx, stateRoot, domainID)
	if err != nil {
		return err
	}
	defer storageLock.Release()
	current, err := session.LoadRecord(stateRoot, string(domainID), string(expected.Name))
	if err != nil {
		return err
	}
	if current != expected {
		return fmt.Errorf("session changed before exact use verification")
	}
	attached, err := listSessionAttachments(ctx, stateRoot, domainID, expected.ID, string(expected.Name))
	if err != nil {
		return err
	}
	want := Use{BackendKind: expected.Backend.Kind, BackendObject: expected.Backend.ObjectID, Generation: expected.StartGeneration}
	for _, record := range attached {
		if record.State != StateAvailable || record.Disk == nil || record.Pending != nil || record.Use == nil || *record.Use != want {
			return fmt.Errorf("workspace %q lacks exact session use", record.VolumeID)
		}
	}
	return nil
}
