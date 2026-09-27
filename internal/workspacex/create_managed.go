package workspacex

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/weshofmann/boxwarden/internal/workspaceformat"
)

// ManagedFormatter must admit its exact trusted helper before any new disk is
// reserved. The helper is not needed when replaying an already verified journal.
type ManagedFormatter interface {
	workspaceformat.Formatter
	Check(context.Context) error
}

// CreateManaged creates one exact volume and promotes it only after formatter
// admission. An existing verified journal can finish record publication after
// a crash, but an incomplete or failed format is never run again under its ID.
func CreateManaged(ctx context.Context, stateRoot string, request workspaceformat.Request, formatter ManagedFormatter) (Record, error) {
	creating := Record{Version: recordVersion, Domain: request.Domain, VolumeID: request.VolumeID,
		SizeBytes: request.SizeBytes, Format: FormatRawExt4, FilesystemUUID: request.FilesystemUUID, State: StateCreating}
	if err := validateRecord(request.Domain, creating); err != nil {
		return Record{}, err
	}
	if formatter == nil {
		return Record{}, fmt.Errorf("workspace formatter is required")
	}
	existing, found, err := loadManagedRecord(ctx, stateRoot, request)
	if err != nil {
		return Record{}, err
	}
	if found {
		return PromoteVerified(ctx, stateRoot, request.Domain, request.VolumeID)
	}

	// A verified journal is sufficient for a retry after formatting succeeded
	// but record publication did not. Admit performs exact inode and ext4 checks.
	file, _, admitErr := workspaceformat.Admit(stateRoot, request)
	if admitErr == nil {
		if err := file.Close(); err != nil {
			return Record{}, err
		}
	} else {
		if err := formatter.Check(ctx); err != nil {
			return Record{}, fmt.Errorf("admit workspace formatter: %w", err)
		}
		if _, err := workspaceformat.Create(ctx, stateRoot, request, formatter); err != nil {
			// Another invocation may have finished the exact journal while this
			// one waited for the storage lock. Only a fresh verified admission
			// permits publication; a failed journal remains immutable evidence.
			file, _, admitted := workspaceformat.Admit(stateRoot, request)
			if admitted != nil {
				return Record{}, fmt.Errorf("format workspace: %w", err)
			}
			if closeErr := file.Close(); closeErr != nil {
				return Record{}, closeErr
			}
		}
	}

	held, err := AcquireStorageOperation(ctx, stateRoot, request.Domain)
	if err != nil {
		return Record{}, err
	}
	existing, err = LoadRecord(stateRoot, request.Domain, request.VolumeID)
	if errors.Is(err, os.ErrNotExist) {
		err = SaveRecord(stateRoot, request.Domain, creating)
	} else if err == nil {
		err = matchManagedRecord(existing, request)
	}
	releaseErr := held.Release()
	if err != nil {
		return Record{}, fmt.Errorf("publish creating workspace record: %w", err)
	}
	if releaseErr != nil {
		return Record{}, fmt.Errorf("release workspace creation lock: %w", releaseErr)
	}
	return PromoteVerified(ctx, stateRoot, request.Domain, request.VolumeID)
}

func loadManagedRecord(ctx context.Context, stateRoot string, request workspaceformat.Request) (Record, bool, error) {
	held, err := AcquireStorageOperation(ctx, stateRoot, request.Domain)
	if err != nil {
		return Record{}, false, err
	}
	record, err := LoadRecord(stateRoot, request.Domain, request.VolumeID)
	releaseErr := held.Release()
	if releaseErr != nil {
		return Record{}, false, fmt.Errorf("release workspace inspection lock: %w", releaseErr)
	}
	if errors.Is(err, os.ErrNotExist) {
		return Record{}, false, nil
	}
	if err != nil {
		return Record{}, false, err
	}
	if err := matchManagedRecord(record, request); err != nil {
		return Record{}, false, err
	}
	return record, true, nil
}

func matchManagedRecord(record Record, request workspaceformat.Request) error {
	if record.Domain != request.Domain || record.VolumeID != request.VolumeID || record.SizeBytes != request.SizeBytes ||
		record.FilesystemUUID != request.FilesystemUUID || record.Format != FormatRawExt4 {
		return fmt.Errorf("workspace creation request differs from existing volume identity")
	}
	if (record.State != StateCreating && record.State != StateAvailable) || record.Attachment != nil || record.Use != nil || record.Pending != nil {
		return fmt.Errorf("existing workspace is not an unbound creatable volume")
	}
	return nil
}
