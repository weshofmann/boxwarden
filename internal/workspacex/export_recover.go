package workspacex

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"

	"github.com/weshofmann/boxwarden/internal/backend"
	"github.com/weshofmann/boxwarden/internal/domain"
	"github.com/weshofmann/boxwarden/internal/lock"
	"github.com/weshofmann/boxwarden/internal/workspaceformat"
)

// RecoverExportSnapshot reconciles only the private copy stage. It never
// removes or republishes receiver output, and a live or uncertain backend
// leaves the volume Pending marker intact. A copying journal is aborted after
// exact partial-file cleanup; a ready journal is re-admitted before Pending
// can clear. Inspector and published phases require their own recovery path.
func RecoverExportSnapshot(ctx context.Context, stateRoot string, domainID domain.ID, transactionID string, observer backend.Observer) (result ExportJournal, err error) {
	initial, err := loadExportJournal(stateRoot, domainID, transactionID)
	if err != nil {
		return ExportJournal{}, err
	}
	if initial.Phase != ExportCopying && initial.Phase != ExportSnapshotReady && initial.Phase != ExportAborted {
		return ExportJournal{}, fmt.Errorf("transaction is beyond snapshot recovery")
	}
	volumeLock, err := AcquireVolumeUse(ctx, stateRoot, domainID, initial.VolumeID)
	if err != nil {
		return ExportJournal{}, err
	}
	defer func() { err = errors.Join(err, volumeLock.Release()) }()
	sessionLock, err := lock.AcquireSession(ctx, stateRoot, string(domainID), initial.SessionName)
	if err != nil {
		return ExportJournal{}, err
	}
	defer func() { err = errors.Join(err, sessionLock.Release()) }()
	storageLock, err := AcquireStorageOperation(ctx, stateRoot, domainID)
	if err != nil {
		return ExportJournal{}, err
	}
	defer func() { err = errors.Join(err, storageLock.Release()) }()
	journal, err := loadExportJournal(stateRoot, domainID, transactionID)
	if err != nil || !reflect.DeepEqual(initial, journal) {
		return ExportJournal{}, fmt.Errorf("export journal changed before recovery: %v", err)
	}
	record, err := LoadRecord(stateRoot, domainID, journal.VolumeID)
	if err != nil {
		return ExportJournal{}, err
	}
	if record.Pending == nil && (journal.Phase == ExportSnapshotReady || journal.Phase == ExportAborted) {
		// This is an idempotent state read, not fresh snapshot admission.
		// The volume may already have restarted or changed attachment.
		return journal, nil
	}
	if record.State != StateAvailable || record.Disk == nil || *record.Disk != journal.Source ||
		record.FilesystemUUID != journal.FilesystemUUID || record.SizeBytes != journal.SizeBytes || record.Use != nil ||
		record.Attachment == nil || record.Attachment.SessionID != journal.SessionID || record.Attachment.SessionName != journal.SessionName ||
		(record.Pending != nil && (record.Pending.Kind != "export-snapshot" || record.Pending.ID != journal.ID)) {
		return ExportJournal{}, fmt.Errorf("export recovery cannot prove exact available workspace binding")
	}
	stopped, err := stoppedSession(ctx, stateRoot, domainID, journal.SessionName, observer)
	if err != nil || stopped.ID != journal.SessionID || stopped.Backend.ObjectID != journal.BackendObject {
		return ExportJournal{}, fmt.Errorf("export recovery cannot prove exact stopped backend: %w", err)
	}
	request := workspaceformat.Request{Domain: domainID, VolumeID: journal.VolumeID, FilesystemUUID: journal.FilesystemUUID, SizeBytes: journal.SizeBytes}
	source, qualified, err := workspaceformat.Admit(stateRoot, request)
	if err != nil {
		return ExportJournal{}, fmt.Errorf("re-admit export source for recovery: %w", err)
	}
	defer source.Close()
	if qualified.Identity.Device != journal.Source.Device || qualified.Identity.Inode != journal.Source.Inode {
		return ExportJournal{}, fmt.Errorf("export source identity changed before recovery")
	}
	switch journal.Phase {
	case ExportCopying:
		if record.Pending == nil {
			// The journal can precede the marker after a crash. In that
			// window no copy was authorized; a directory is ambiguous.
			if err := requireAbsentSnapshotDirectory(stateRoot, journal.ID); err != nil {
				return ExportJournal{}, err
			}
		} else {
			if err := removeExactPartialSnapshot(stateRoot, journal); err != nil {
				return ExportJournal{}, err
			}
		}
		aborted := journal
		aborted.Phase = ExportAborted
		if err := advanceExportJournal(ctx, stateRoot, journal, aborted); err != nil {
			return ExportJournal{}, err
		}
		journal = aborted
	case ExportSnapshotReady:
		if record.Pending != nil {
			if err := admitExactExportSnapshot(ctx, stateRoot, journal); err != nil {
				return ExportJournal{}, err
			}
			if err := finishExportSnapshotPending(stateRoot, domainID, journal); err != nil {
				return ExportJournal{}, err
			}
		}
		return journal, nil
	case ExportAborted:
		// Cleanup was durable before the aborted phase was published. Any
		// reappearing copy is ambiguous and cannot be removed on retry.
		if err := requireAbsentSnapshotDirectory(stateRoot, journal.ID); err != nil {
			return ExportJournal{}, err
		}
	}
	if record.Pending != nil {
		record.Pending = nil
		if err := saveRecordTransitionExpectedPending(stateRoot, domainID, record, mutationAbortExportSnapshot, journal.ID, nil); err != nil {
			return ExportJournal{}, err
		}
	}
	return journal, nil
}

func removeExactPartialSnapshot(stateRoot string, journal ExportJournal) error {
	root, err := openStateRoot(stateRoot)
	if err != nil {
		return err
	}
	defer root.Close()
	exports, err := openChild(root, "exports", false)
	if err != nil {
		return err
	}
	defer exports.Close()
	dir, err := openChild(exports, journal.ID, false)
	if errors.Is(err, os.ErrNotExist) {
		return nil // crash after journal creation, before snapshot directory
	}
	if err != nil {
		return err
	}
	listing, err := dir.Open(".")
	if err != nil {
		dir.Close()
		return err
	}
	entries, readErr := listing.ReadDir(-1)
	closeErr := listing.Close()
	if err := errors.Join(readErr, closeErr); err != nil {
		dir.Close()
		return err
	}
	for _, entry := range entries {
		if entry.Name() != "snapshot.raw" {
			dir.Close()
			return fmt.Errorf("export transaction directory contains unexpected state")
		}
	}
	info, err := dir.Lstat("snapshot.raw")
	if err == nil {
		if privateRegular(info) != nil || info.Size() > journal.SizeBytes || checkPrivateACL(filepath.Join(dir.Name(), "snapshot.raw"), info) != nil {
			dir.Close()
			return fmt.Errorf("partial export snapshot is not exact private owned state")
		}
		if err := dir.Remove("snapshot.raw"); err != nil {
			dir.Close()
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		dir.Close()
		return err
	}
	if err := syncDirectory(dir); err != nil {
		dir.Close()
		return err
	}
	if err := dir.Close(); err != nil {
		return err
	}
	if err := exports.Remove(journal.ID); err != nil {
		return fmt.Errorf("export transaction directory has unexpected state: %w", err)
	}
	return syncDirectory(exports)
}

func requireAbsentSnapshotDirectory(stateRoot, id string) error {
	root, err := openStateRoot(stateRoot)
	if err != nil {
		return err
	}
	defer root.Close()
	exports, err := openChild(root, "exports", false)
	if err != nil {
		return err
	}
	defer exports.Close()
	if _, err := exports.Lstat(id); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	return fmt.Errorf("export snapshot directory exists without exact cleanup authority")
}

func admitExactExportSnapshot(ctx context.Context, stateRoot string, journal ExportJournal) error {
	root, err := openStateRoot(stateRoot)
	if err != nil {
		return err
	}
	defer root.Close()
	exports, err := openChild(root, "exports", false)
	if err != nil {
		return err
	}
	defer exports.Close()
	dir, err := openChild(exports, journal.ID, false)
	if err != nil {
		return err
	}
	defer dir.Close()
	file, err := openPrivateFile(dir, "snapshot.raw")
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || info.Size() != journal.SizeBytes {
		return fmt.Errorf("snapshot size changed before recovery: %v", err)
	}
	identity, err := diskIdentity(info)
	if err != nil || journal.Snapshot == nil || identity != journal.Snapshot.Identity || identity == journal.Source {
		return fmt.Errorf("snapshot identity changed before recovery: %v", err)
	}
	hash, err := hashExactExportBytes(ctx, file, journal.SizeBytes)
	if err != nil || hex.EncodeToString(hash) != journal.Snapshot.SHA256 {
		return fmt.Errorf("snapshot digest changed before recovery: %v", err)
	}
	finalInfo, err := dir.Lstat("snapshot.raw")
	if err != nil || !os.SameFile(info, finalInfo) || finalInfo.Size() != journal.SizeBytes || privateRegular(finalInfo) != nil {
		return fmt.Errorf("snapshot path changed before recovery: %v", err)
	}
	finalOpened, err := file.Stat()
	if err != nil || !os.SameFile(finalOpened, finalInfo) || finalOpened.Size() != journal.SizeBytes || privateRegular(finalOpened) != nil {
		return fmt.Errorf("snapshot descriptor changed before recovery: %v", err)
	}
	if err := checkPrivateACL(filepath.Join(dir.Name(), "snapshot.raw"), finalInfo); err != nil {
		return err
	}
	return nil
}
