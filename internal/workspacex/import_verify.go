package workspacex

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"syscall"

	"github.com/weshofmann/boxwarden/internal/backend"
	"github.com/weshofmann/boxwarden/internal/domain"
	"github.com/weshofmann/boxwarden/internal/importx"
	"github.com/weshofmann/boxwarden/internal/lock"
	"github.com/weshofmann/boxwarden/internal/workspaceformat"
)

// VerifyStoppedImport binds one complete selected export of the imported
// transaction directory to the exact stopped workspace and captured source.
// Verified means those bytes persisted through that stop and export, not that
// a future guest action cannot modify the workspace.
func VerifyStoppedImport(ctx context.Context, stateRoot string, domainID domain.ID, importID, exportID string, observer backend.Observer) (result ImportJournal, err error) {
	if !validUUID(importID) || !validUUID(exportID) || importID == exportID || observer == nil {
		return ImportJournal{}, fmt.Errorf("invalid stopped import verification request")
	}
	initial, err := LoadImportJournal(stateRoot, domainID, importID)
	if err != nil {
		return ImportJournal{}, err
	}
	if initial.Phase != ImportTransferring && (initial.Phase != ImportVerified || initial.ExportID != exportID) {
		return ImportJournal{}, fmt.Errorf("import is not transferring or verified by this export")
	}
	volumeLock, err := AcquireVolumeUse(ctx, stateRoot, domainID, initial.VolumeID)
	if err != nil {
		return ImportJournal{}, err
	}
	defer func() { err = errors.Join(err, volumeLock.Release()) }()
	sessionLock, err := lock.AcquireSession(ctx, stateRoot, string(domainID), initial.SessionName)
	if err != nil {
		return ImportJournal{}, err
	}
	defer func() { err = errors.Join(err, sessionLock.Release()) }()
	storageLock, err := AcquireStorageOperation(ctx, stateRoot, domainID)
	if err != nil {
		return ImportJournal{}, err
	}
	defer func() { err = errors.Join(err, storageLock.Release()) }()
	current, err := LoadImportJournal(stateRoot, domainID, importID)
	if err != nil || current != initial {
		return ImportJournal{}, fmt.Errorf("import journal changed before verification: %v", err)
	}
	exported, err := loadExportJournal(stateRoot, domainID, exportID)
	if err != nil {
		return ImportJournal{}, err
	}
	if err := admitStoppedImportExport(ctx, stateRoot, current, exported, observer); err != nil {
		return ImportJournal{}, err
	}
	if err := admitExactExportSnapshot(ctx, stateRoot, exported); err != nil {
		return ImportJournal{}, fmt.Errorf("re-admit stopped export snapshot: %w", err)
	}
	publishedRoot, err := publishedImportRoot(exported, current.ID)
	if err != nil {
		return ImportJournal{}, err
	}
	snapshot, err := importx.ComparePublishedTree(filepath.Join(stateRoot, "imports"), current.ID, publishedRoot)
	if err != nil {
		return ImportJournal{}, fmt.Errorf("published import differs from captured source: %w", err)
	}
	if snapshot.Digest != current.SourceDigest || snapshot.FileCount != current.FileCount || snapshot.TotalBytes != current.TotalBytes {
		return ImportJournal{}, fmt.Errorf("captured source differs from import journal")
	}
	if againPath, pathErr := publishedImportRoot(exported, current.ID); pathErr != nil || againPath != publishedRoot {
		return ImportJournal{}, fmt.Errorf("published import destination changed during verification: %v", pathErr)
	}
	again, err := loadExportJournal(stateRoot, domainID, exportID)
	if err != nil || !sameExportJournal(again, exported) {
		return ImportJournal{}, fmt.Errorf("export evidence changed during import verification: %v", err)
	}
	if err := admitStoppedImportExport(ctx, stateRoot, current, exported, observer); err != nil {
		return ImportJournal{}, err
	}
	if current.Phase == ImportVerified {
		return current, nil
	}
	verified := current
	verified.Phase = ImportVerified
	verified.ExportID = exportID
	if err := advanceImportJournal(ctx, stateRoot, current, verified); err != nil {
		return ImportJournal{}, err
	}
	return verified, nil
}

func admitStoppedImportExport(ctx context.Context, stateRoot string, imported ImportJournal, exported ExportJournal, observer backend.Observer) error {
	rootName := "boxwarden-import-" + imported.ID
	if exported.Phase != ExportPublished || exported.Domain != imported.Domain || exported.VolumeID != imported.VolumeID ||
		exported.SessionID != imported.SessionID || exported.SessionName != imported.SessionName ||
		exported.BackendObject != imported.BackendObject || exported.FilesystemUUID != imported.FilesystemUUID ||
		len(exported.Selected) != 1 || exported.Selected[0] != rootName {
		return fmt.Errorf("published export does not bind the complete imported directory")
	}
	volume, err := LoadRecord(stateRoot, imported.Domain, imported.VolumeID)
	if err != nil {
		return err
	}
	if volume.State != StateAvailable || volume.Disk == nil || volume.Attachment == nil || volume.Use != nil || volume.Pending != nil ||
		volume.FilesystemUUID != imported.FilesystemUUID || volume.SizeBytes != exported.SizeBytes || *volume.Disk != exported.Source ||
		volume.Attachment.SessionID != imported.SessionID || volume.Attachment.SessionName != imported.SessionName || volume.Attachment.MountPath != imported.MountPath {
		return fmt.Errorf("stopped export does not bind the retained workspace disk")
	}
	request := workspaceformat.Request{Domain: imported.Domain, VolumeID: imported.VolumeID, FilesystemUUID: imported.FilesystemUUID, SizeBytes: volume.SizeBytes}
	disk, qualified, err := workspaceformat.Admit(stateRoot, request)
	if err != nil {
		return fmt.Errorf("re-admit retained import volume: %w", err)
	}
	if closeErr := disk.Close(); closeErr != nil {
		return closeErr
	}
	if qualified.Identity.Device != volume.Disk.Device || qualified.Identity.Inode != volume.Disk.Inode {
		return fmt.Errorf("retained import disk identity changed")
	}
	stopped, err := stoppedSession(ctx, stateRoot, imported.Domain, imported.SessionName, observer)
	if err != nil {
		return err
	}
	if stopped.ID != imported.SessionID || stopped.Backend.ObjectID != imported.BackendObject || stopped.Domain != imported.Domain {
		return fmt.Errorf("stopped session differs from import and export")
	}
	return nil
}

func publishedImportRoot(exported ExportJournal, importID string) (string, error) {
	parent, err := os.Lstat(exported.DestinationParent)
	if err != nil || privateDirectory(parent) != nil {
		return "", fmt.Errorf("published export parent is not private: %v", err)
	}
	if err := checkPrivateACL(exported.DestinationParent, parent); err != nil {
		return "", err
	}
	directory, err := os.OpenFile(exported.DestinationParent, os.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return "", err
	}
	defer directory.Close()
	opened, err := directory.Stat()
	if err != nil || !os.SameFile(parent, opened) {
		return "", fmt.Errorf("published export parent changed while opening: %v", err)
	}
	identity, err := diskIdentity(opened)
	if err != nil || identity != exported.Destination {
		return "", fmt.Errorf("published export parent identity changed: %v", err)
	}
	entries, err := directory.ReadDir(2)
	if err != nil || len(entries) != 1 || entries[0].Name() != exportTransactionHex(exported.ID) || !entries[0].IsDir() {
		return "", fmt.Errorf("published export destination is not the exact sole transaction directory: %v", err)
	}
	closed, err := os.Lstat(exported.DestinationParent)
	if err != nil || !os.SameFile(opened, closed) {
		return "", fmt.Errorf("published export parent changed during admission: %v", err)
	}
	transaction := filepath.Join(exported.DestinationParent, exportTransactionHex(exported.ID))
	transactionInfo, err := os.Lstat(transaction)
	if err != nil || privateDirectory(transactionInfo) != nil {
		return "", fmt.Errorf("published export transaction directory is not private: %v", err)
	}
	if err := checkPrivateACL(transaction, transactionInfo); err != nil {
		return "", err
	}
	return filepath.Join(transaction, "boxwarden-import-"+importID), nil
}

func sameExportJournal(a, b ExportJournal) bool {
	return reflect.DeepEqual(a, b)
}
