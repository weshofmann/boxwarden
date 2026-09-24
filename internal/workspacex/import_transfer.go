package workspacex

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/weshofmann/boxwarden/internal/domain"
	"github.com/weshofmann/boxwarden/internal/lock"
	"github.com/weshofmann/boxwarden/internal/session"
	"github.com/weshofmann/boxwarden/internal/supervisor"
)

// ImportTransferController exposes only the exact retained generation's
// snapshot and typed transfer action. It never returns SSH credentials.
type ImportTransferController interface {
	Snapshot(context.Context, supervisor.Binding) (supervisor.Snapshot, error)
	TransferImport(context.Context, supervisor.Binding, supervisor.ImportTransfer) (supervisor.ImportResult, error)
}

// TransferCapturedImport advances a captured journal to transferring before
// any guest mutation. It holds the transition and session locks across the
// bounded supervisor RPC, then rechecks durable binding and fresh readiness.
// A successful result remains transferring until a separate persistence gate.
func TransferCapturedImport(ctx context.Context, stateRoot string, domainID domain.ID, transactionID string, controller ImportTransferController) (result supervisor.ImportResult, err error) {
	if controller == nil || !validUUID(transactionID) {
		return supervisor.ImportResult{}, fmt.Errorf("invalid import transfer request")
	}
	initial, err := LoadImportJournal(stateRoot, domainID, transactionID)
	if err != nil {
		return supervisor.ImportResult{}, err
	}
	transition, err := lock.Acquire(ctx, stateRoot, "transition-"+string(domainID)+"-"+initial.SessionName)
	if err != nil {
		return supervisor.ImportResult{}, err
	}
	defer func() { err = errors.Join(err, transition.Release()) }()
	sessionLock, err := lock.AcquireSession(ctx, stateRoot, string(domainID), initial.SessionName)
	if err != nil {
		return supervisor.ImportResult{}, err
	}
	defer func() { err = errors.Join(err, sessionLock.Release()) }()
	storageLock, err := AcquireStorageOperation(ctx, stateRoot, domainID)
	if err != nil {
		return supervisor.ImportResult{}, err
	}
	defer func() {
		if storageLock != nil {
			err = errors.Join(err, storageLock.Release())
		}
	}()
	current, err := LoadImportJournal(stateRoot, domainID, transactionID)
	if err != nil || current != initial {
		return supervisor.ImportResult{}, fmt.Errorf("import journal changed before transfer: %v", err)
	}
	if current.Phase != ImportCaptured && current.Phase != ImportTransferring {
		return supervisor.ImportResult{}, fmt.Errorf("import is not captured or transferring")
	}
	if err := admitTransferLive(ctx, stateRoot, current, controller); err != nil {
		return supervisor.ImportResult{}, err
	}
	if current.Phase == ImportCaptured {
		next := current
		next.Phase = ImportTransferring
		if err := advanceImportJournal(ctx, stateRoot, current, next); err != nil {
			return supervisor.ImportResult{}, err
		}
		current = next
	}
	released := storageLock
	storageLock = nil
	if err := released.Release(); err != nil {
		return supervisor.ImportResult{}, err
	}
	binding, spec := importControlBinding(current)
	result, err = controller.TransferImport(ctx, binding, spec)
	if err != nil {
		return supervisor.ImportResult{}, err
	}
	storageLock, err = AcquireStorageOperation(ctx, stateRoot, domainID)
	if err != nil {
		return supervisor.ImportResult{}, err
	}
	after, err := LoadImportJournal(stateRoot, domainID, transactionID)
	if err != nil || after != current {
		return supervisor.ImportResult{}, fmt.Errorf("import journal changed during transfer: %v", err)
	}
	if err := admitTransferLive(ctx, stateRoot, current, controller); err != nil {
		return supervisor.ImportResult{}, err
	}
	if result.Digest != current.SourceDigest || result.FileCount != current.FileCount || result.TotalBytes != current.TotalBytes ||
		result.RemotePath != current.MountPath+"/boxwarden-import-"+current.ID {
		return supervisor.ImportResult{}, fmt.Errorf("import readback differs from captured transaction")
	}
	return result, nil
}

func importControlBinding(j ImportJournal) (supervisor.Binding, supervisor.ImportTransfer) {
	return supervisor.Binding{Domain: string(j.Domain), SessionID: j.SessionID, BackendKind: "tart", BackendObject: j.BackendObject, Generation: j.Generation},
		supervisor.ImportTransfer{TransactionID: j.ID, SourceDigest: j.SourceDigest, VolumeID: j.VolumeID, FilesystemUUID: j.FilesystemUUID, MountPath: j.MountPath}
}

func admitTransferLive(ctx context.Context, stateRoot string, j ImportJournal, controller ImportTransferController) error {
	if err := admitImportSnapshot(stateRoot, j); err != nil {
		return fmt.Errorf("captured import source changed: %w", err)
	}
	current, err := session.LoadRecord(stateRoot, string(j.Domain), j.SessionName)
	if err != nil {
		return err
	}
	if current.Domain != j.Domain || current.Version != 2 || current.Mode != session.ModeClean || current.IntendedState != session.StateRunning ||
		current.Readiness.Status != session.ReadinessReady || current.ID != j.SessionID || current.Backend.Kind != "tart" ||
		current.Backend.ObjectID != j.BackendObject || current.StartGeneration != j.Generation {
		return fmt.Errorf("durable session differs from exact import generation")
	}
	if err := session.RequireNoRebuild(stateRoot, j.Domain, j.SessionName); err != nil {
		return err
	}
	volume, err := LoadRecord(stateRoot, j.Domain, j.VolumeID)
	if err != nil {
		return err
	}
	if volume.State != StateAvailable || volume.Disk == nil || volume.Pending != nil || volume.Attachment == nil || volume.Use == nil ||
		volume.FilesystemUUID != j.FilesystemUUID || volume.Attachment.SessionID != j.SessionID || volume.Attachment.SessionName != j.SessionName ||
		volume.Attachment.MountPath != j.MountPath || *volume.Use != (Use{BackendKind: "tart", BackendObject: j.BackendObject, Generation: j.Generation}) {
		return fmt.Errorf("workspace lacks exact active import Use")
	}
	binding, _ := importControlBinding(j)
	live, err := controller.Snapshot(ctx, binding)
	if err != nil {
		return err
	}
	now := time.Now()
	if live.Binding != binding || live.ObservedAt.IsZero() || live.ObservedAt.After(now) || now.Sub(live.ObservedAt) > maxImportSnapshotAge ||
		!live.BackendRunning || !live.SerialHealthy || !live.PinPresent || !live.CertificateCurrent || !live.ProbeOK || !live.ZoneMatches {
		return fmt.Errorf("fresh exact import readiness is unproven")
	}
	return ctx.Err()
}
