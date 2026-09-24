package workspacex

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"

	"github.com/weshofmann/boxwarden/internal/domain"
)

const maxRecordBytes = 1 << 20

type storeStage string
type mutationKind uint8

const (
	mutationGeneric mutationKind = iota
	mutationAttach
	mutationDetach
	mutationReserveUse
	mutationReleaseUse
	mutationPromoteVerified
	mutationBeginExportSnapshot
	mutationFinishExportSnapshot
)

const (
	storeBeforeRename storeStage = "before_rename"
	storeAfterRename  storeStage = "after_rename"
)

// SaveRecord atomically replaces a volume record. It can reserve a new
// unattached volume and persist bounded non-attachment state, but it cannot
// qualify a disk, change an attachment or use reservation, or clear a pending
// operation. Callers hold the domain storage-operation lock; operations that
// also touch a sandbox acquire its session lock first.
func SaveRecord(stateRoot string, expectedDomain domain.ID, record Record) error {
	return saveRecord(stateRoot, expectedDomain, record, nil)
}

func saveRecord(stateRoot string, expectedDomain domain.ID, record Record, hook func(storeStage) error) error {
	return saveRecordTransition(stateRoot, expectedDomain, record, mutationGeneric, hook)
}

func saveRecordTransition(stateRoot string, expectedDomain domain.ID, record Record, mutation mutationKind, hook func(storeStage) error) error {
	return saveRecordTransitionExpectedPending(stateRoot, expectedDomain, record, mutation, "", hook)
}

func saveRecordTransitionExpectedPending(stateRoot string, expectedDomain domain.ID, record Record, mutation mutationKind, expectedPendingID string, hook func(storeStage) error) error {
	if err := validateRecord(expectedDomain, record); err != nil {
		return err
	}
	root, err := openStateRoot(stateRoot)
	if err != nil {
		return fmt.Errorf("state root: %w", err)
	}
	defer root.Close()
	workspaces, err := openChild(root, "workspaces", true)
	if err != nil {
		return fmt.Errorf("workspace directory: %w", err)
	}
	defer workspaces.Close()
	if err := checkBindings(workspaces, stateRoot, expectedDomain, record, mutation, expectedPendingID); err != nil {
		return err
	}
	name := record.VolumeID + ".json"
	if info, err := workspaces.Lstat(name); err == nil {
		if err := privateRegular(info); err != nil {
			return fmt.Errorf("workspace record: %w", err)
		}
		if err := checkPrivateACL(filepath.Join(workspaces.Name(), name), info); err != nil {
			return fmt.Errorf("workspace record ACL: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	raw, err := json.Marshal(record)
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	if len(raw) > maxRecordBytes {
		return fmt.Errorf("workspace record exceeds 1 MiB")
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return err
	}
	temporaryName := name + ".tmp-" + hex.EncodeToString(nonce[:])
	temporary, err := workspaces.OpenFile(temporaryName, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return fmt.Errorf("create temporary workspace record: %w", err)
	}
	defer func() { _ = workspaces.Remove(temporaryName) }()
	temporaryInfo, err := temporary.Stat()
	if err != nil {
		temporary.Close()
		return err
	}
	if err := privateRegular(temporaryInfo); err != nil {
		temporary.Close()
		return err
	}
	if err := checkPrivateACL(filepath.Join(workspaces.Name(), temporaryName), temporaryInfo); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(raw); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if hook != nil {
		if err := hook(storeBeforeRename); err != nil {
			return err
		}
	}
	if err := workspaces.Rename(temporaryName, name); err != nil {
		return err
	}
	publishedInfo, err := workspaces.Lstat(name)
	if err != nil || !os.SameFile(temporaryInfo, publishedInfo) {
		return fmt.Errorf("workspace record changed during publication: %v", err)
	}
	if err := privateRegular(publishedInfo); err != nil {
		return err
	}
	if err := checkPrivateACL(filepath.Join(workspaces.Name(), name), publishedInfo); err != nil {
		return fmt.Errorf("published workspace record ACL: %w", err)
	}
	if hook != nil {
		if err := hook(storeAfterRename); err != nil {
			return err
		}
	}
	if err := syncDirectory(workspaces); err != nil {
		return err
	}
	return nil
}

func checkBindings(workspaces *os.Root, stateRoot string, expectedDomain domain.ID, next Record, mutation mutationKind, expectedPendingID string) error {
	directory, err := workspaces.Open(".")
	if err != nil {
		return err
	}
	entries, readErr := directory.ReadDir(-1)
	closeErr := directory.Close()
	if readErr != nil {
		return readErr
	}
	if closeErr != nil {
		return closeErr
	}
	found := false
	for _, entry := range entries {
		name := entry.Name()
		if filepath.Ext(name) != ".json" {
			continue
		}
		key := name[:len(name)-len(".json")]
		if !validUUID(key) {
			return fmt.Errorf("invalid workspace record name %q", name)
		}
		existing, err := loadFromRoot(workspaces, expectedDomain, key)
		if err != nil {
			return fmt.Errorf("inspect workspace record %q: %w", name, err)
		}
		if existing.VolumeID == next.VolumeID {
			found = true
			if existing.State == StateCreating && next.State == StateAvailable && mutation != mutationPromoteVerified {
				return fmt.Errorf("available promotion requires qualified exclusive creation and ext4 verification")
			}
			if existing.State == StateFailed && next.State != StateFailed ||
				existing.State == StateAvailable && next.State == StateCreating {
				return fmt.Errorf("unsafe workspace state regression")
			}
			if existing.SizeBytes != next.SizeBytes || existing.Format != next.Format || existing.FilesystemUUID != next.FilesystemUUID {
				return fmt.Errorf("workspace volume and filesystem identity is immutable")
			}
			if existing.Disk != nil && (next.Disk == nil || *existing.Disk != *next.Disk) {
				return fmt.Errorf("workspace disk identity is immutable")
			}
			if existing.Pending != nil && !reflect.DeepEqual(existing.Pending, next.Pending) && mutation != mutationFinishExportSnapshot {
				return fmt.Errorf("pending workspace operation requires explicit reconciliation")
			}
			switch mutation {
			case mutationPromoteVerified:
				if existing.State != StateCreating || next.State != StateAvailable || existing.Disk != nil || next.Disk == nil || existing.Pending != nil || next.Pending != nil || existing.Attachment != nil || next.Attachment != nil || existing.Use != nil || next.Use != nil {
					return fmt.Errorf("invalid verified workspace promotion")
				}
			case mutationGeneric:
				if !reflect.DeepEqual(existing.Attachment, next.Attachment) || !reflect.DeepEqual(existing.Use, next.Use) {
					return fmt.Errorf("attachment or use requires a dedicated observed transition")
				}
				if existing.Pending == nil && next.Pending != nil && next.Pending.Kind == "export-snapshot" {
					return fmt.Errorf("export snapshot requires a dedicated stopped-volume transition")
				}
			case mutationBeginExportSnapshot:
				unchanged := existing
				unchanged.Pending = next.Pending
				if existing.State != StateAvailable || existing.Attachment == nil || existing.Use != nil || existing.Pending != nil ||
					next.Pending == nil || next.Pending.Kind != "export-snapshot" || !reflect.DeepEqual(unchanged, next) {
					return fmt.Errorf("invalid export snapshot reservation")
				}
			case mutationFinishExportSnapshot:
				unchanged := existing
				unchanged.Pending = nil
				if existing.Pending == nil || existing.Pending.Kind != "export-snapshot" || existing.Pending.ID != expectedPendingID || next.Pending != nil || !reflect.DeepEqual(unchanged, next) {
					return fmt.Errorf("invalid export snapshot completion")
				}
				journal, err := loadExportJournal(stateRoot, expectedDomain, expectedPendingID)
				if err != nil || journal.Phase != ExportSnapshotReady || journal.Snapshot == nil || journal.VolumeID != existing.VolumeID ||
					journal.Source != *existing.Disk || journal.SessionID != existing.Attachment.SessionID || journal.SessionName != existing.Attachment.SessionName {
					return fmt.Errorf("export snapshot lacks exact durable ready journal: %v", err)
				}
			case mutationAttach:
				if existing.Attachment != nil || next.Attachment == nil || existing.Use != nil || next.Use != nil {
					return fmt.Errorf("invalid attach transition")
				}
			case mutationDetach:
				if existing.Attachment == nil || next.Attachment != nil || existing.Use != nil || next.Use != nil {
					return fmt.Errorf("invalid detach transition")
				}
			case mutationReserveUse:
				if existing.Use != nil || next.Use == nil || existing.Attachment == nil || !reflect.DeepEqual(existing.Attachment, next.Attachment) {
					return fmt.Errorf("invalid use-reservation transition")
				}
			case mutationReleaseUse:
				if existing.Use == nil || next.Use != nil || !reflect.DeepEqual(existing.Attachment, next.Attachment) {
					return fmt.Errorf("invalid use-release transition")
				}
			default:
				return fmt.Errorf("unknown workspace mutation")
			}
		} else if next.Attachment != nil && existing.Attachment != nil && existing.Attachment.SessionID == next.Attachment.SessionID && mountsOverlap(existing.Attachment.MountPath, next.Attachment.MountPath) {
			return fmt.Errorf("guest mount path overlaps another workspace")
		}
	}
	if !found && (mutation != mutationGeneric || next.State != StateCreating || next.Attachment != nil || next.Use != nil || next.Disk != nil) {
		return fmt.Errorf("new workspace must start unbound in creating state")
	}
	return nil
}

func mountsOverlap(a, b string) bool {
	return a == b || strings.HasPrefix(a, b+"/") || strings.HasPrefix(b, a+"/")
}

// LoadRecord performs a read-only, strict decode for exactly one volume UUID.
func LoadRecord(stateRoot string, expectedDomain domain.ID, volumeID string) (Record, error) {
	if !validUUID(volumeID) {
		return Record{}, fmt.Errorf("invalid volume ID")
	}
	if _, err := domain.Parse(string(expectedDomain)); err != nil {
		return Record{}, err
	}
	root, err := openStateRoot(stateRoot)
	if err != nil {
		return Record{}, err
	}
	defer root.Close()
	workspaces, err := openChild(root, "workspaces", false)
	if err != nil {
		return Record{}, err
	}
	defer workspaces.Close()
	return loadFromRoot(workspaces, expectedDomain, volumeID)
}

func loadFromRoot(workspaces *os.Root, expectedDomain domain.ID, volumeID string) (Record, error) {
	file, err := openPrivateFile(workspaces, volumeID+".json")
	if err != nil {
		return Record{}, err
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, maxRecordBytes+1))
	if err != nil {
		return Record{}, err
	}
	if len(raw) > maxRecordBytes {
		return Record{}, fmt.Errorf("workspace record exceeds 1 MiB")
	}
	record, err := decodeRecord(bytes.TrimSpace(raw), expectedDomain)
	if err != nil {
		return Record{}, err
	}
	if record.VolumeID != volumeID {
		return Record{}, fmt.Errorf("workspace record volume ID does not match key")
	}
	return record, nil
}

// ManagedDiskPath returns the single allowed host path for a volume ID. The
// path is advisory: callers must pin and recheck the file before VM attachment.
func ManagedDiskPath(stateRoot, volumeID string) (string, error) {
	if !validUUID(volumeID) {
		return "", fmt.Errorf("invalid volume ID")
	}
	root, err := openStateRoot(stateRoot)
	if err != nil {
		return "", err
	}
	defer root.Close()
	if info, err := root.Lstat("volumes"); err == nil {
		if err := privateDirectory(info); err != nil {
			return "", err
		}
		if err := checkPrivateACL(filepath.Join(root.Name(), "volumes"), info); err != nil {
			return "", err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	return filepath.Join(stateRoot, "volumes", volumeID+".raw"), nil
}

// InspectManagedDisk reads an existing managed path for recovery diagnostics.
// It proves file properties, not provenance; creation must use an exclusive
// host-owned operation before any record is promoted to available.
func InspectManagedDisk(stateRoot, volumeID string, sizeBytes int64) (DiskIdentity, error) {
	file, info, err := openDisk(stateRoot, volumeID, sizeBytes)
	if err != nil {
		return DiskIdentity{}, err
	}
	defer file.Close()
	return diskIdentity(info)
}

// AdmitDisk is deliberately closed until a trusted formatter path can prove
// exclusive creation, exact ext4 UUID, and successful post-format verification.
// InspectManagedDisk verifies host file properties only; it cannot qualify a
// filesystem or make an arbitrary raw file safe to attach.
func AdmitDisk(stateRoot string, record Record) (*os.File, error) {
	return nil, fmt.Errorf("workspace disk launch admission requires qualified exclusive creation and ext4 verification")
}

func openDisk(stateRoot, volumeID string, sizeBytes int64) (*os.File, os.FileInfo, error) {
	if !validUUID(volumeID) || sizeBytes < 4096 || sizeBytes > 1<<43 || sizeBytes%512 != 0 {
		return nil, nil, fmt.Errorf("invalid managed disk request")
	}
	root, err := openStateRoot(stateRoot)
	if err != nil {
		return nil, nil, err
	}
	defer root.Close()
	volumes, err := openChild(root, "volumes", false)
	if err != nil {
		return nil, nil, err
	}
	defer volumes.Close()
	file, err := openPrivateFile(volumes, volumeID+".raw")
	if err != nil {
		return nil, nil, err
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, nil, err
	}
	if info.Size() != sizeBytes {
		file.Close()
		return nil, nil, fmt.Errorf("managed disk size changed: got %d, expected %d", info.Size(), sizeBytes)
	}
	return file, info, nil
}
