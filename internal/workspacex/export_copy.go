package workspacex

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"syscall"
	"time"

	"github.com/weshofmann/boxwarden/internal/backend"
	"github.com/weshofmann/boxwarden/internal/diskreserve"
	"github.com/weshofmann/boxwarden/internal/domain"
	"github.com/weshofmann/boxwarden/internal/lock"
	"github.com/weshofmann/boxwarden/internal/workspaceformat"
)

const exportCopyDeadline = 5 * time.Minute
const exportCopyBufferBytes = 1 << 20

// CreateExportSnapshot copies one qualified, stopped workspace into private
// transaction state. It does not start the inspector or publish returned files.
// Failure after reservation deliberately leaves Pending and journal evidence
// for a separate exact recovery operation.
func CreateExportSnapshot(ctx context.Context, stateRoot string, domainID domain.ID, volumeID, destinationParent string, selected []string, observer backend.Observer) (ExportJournal, error) {
	return createExportSnapshot(ctx, stateRoot, domainID, volumeID, destinationParent, selected, observer, copyExportSnapshot)
}

type exportCopier func(context.Context, string, workspaceformat.Request, *os.File, *os.Root, ExportJournal) (ExportSnapshot, error)

func createExportSnapshot(ctx context.Context, stateRoot string, domainID domain.ID, volumeID, destinationParent string, selected []string, observer backend.Observer, copier exportCopier) (result ExportJournal, err error) {
	if copier == nil {
		return ExportJournal{}, fmt.Errorf("nil export copier")
	}
	if !validUUID(volumeID) {
		return ExportJournal{}, fmt.Errorf("invalid export volume ID")
	}
	initial, err := LoadRecord(stateRoot, domainID, volumeID)
	if err != nil {
		return ExportJournal{}, err
	}
	if initial.Attachment == nil || initial.SizeBytes > maxAlphaExportVolumeBytes {
		return ExportJournal{}, fmt.Errorf("export requires an attached alpha-size workspace")
	}
	volumeLock, err := AcquireVolumeUse(ctx, stateRoot, domainID, volumeID)
	if err != nil {
		return ExportJournal{}, err
	}
	defer func() { err = errors.Join(err, volumeLock.Release()) }()
	sessionLock, err := lock.AcquireSession(ctx, stateRoot, string(domainID), initial.Attachment.SessionName)
	if err != nil {
		return ExportJournal{}, err
	}
	defer func() { err = errors.Join(err, sessionLock.Release()) }()
	storageLock, err := AcquireStorageOperation(ctx, stateRoot, domainID)
	if err != nil {
		return ExportJournal{}, err
	}
	defer func() { err = errors.Join(err, storageLock.Release()) }()
	record, err := LoadRecord(stateRoot, domainID, volumeID)
	if err != nil {
		return ExportJournal{}, err
	}
	if record.State != StateAvailable || record.Attachment == nil || *record.Attachment != *initial.Attachment ||
		record.Disk == nil || record.Use != nil || record.Pending != nil || record.SizeBytes > maxAlphaExportVolumeBytes {
		return ExportJournal{}, fmt.Errorf("workspace is not available for stopped export")
	}
	target, err := stoppedSession(ctx, stateRoot, domainID, record.Attachment.SessionName, observer)
	if err != nil || target.ID != record.Attachment.SessionID {
		return ExportJournal{}, fmt.Errorf("export session is not the exact stopped attachment: %w", err)
	}
	request := workspaceformat.Request{Domain: domainID, VolumeID: volumeID, FilesystemUUID: record.FilesystemUUID, SizeBytes: record.SizeBytes}
	source, qualified, err := workspaceformat.Admit(stateRoot, request)
	if err != nil {
		return ExportJournal{}, fmt.Errorf("admit export source: %w", err)
	}
	defer source.Close()
	if qualified.Identity.Device != record.Disk.Device || qualified.Identity.Inode != record.Disk.Inode {
		return ExportJournal{}, fmt.Errorf("workspace source identity differs from qualified record")
	}
	parent, parentIdentity, err := admitExportParent(destinationParent)
	if err != nil {
		return ExportJournal{}, err
	}
	defer parent.Close()
	stateDir, err := os.OpenFile(stateRoot, os.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return ExportJournal{}, err
	}
	defer stateDir.Close()
	if err := checkExportHeadroom(stateDir, parent); err != nil {
		return ExportJournal{}, err
	}
	id, err := newExportUUID()
	if err != nil {
		return ExportJournal{}, err
	}
	journal := ExportJournal{
		Version: 1, ID: id, Domain: domainID, VolumeID: volumeID,
		SessionID: target.ID, SessionName: string(target.Name), BackendObject: target.Backend.ObjectID,
		FilesystemUUID: record.FilesystemUUID, SizeBytes: record.SizeBytes,
		Source: *record.Disk, SnapshotPath: filepath.Join("exports", id, "snapshot.raw"),
		DestinationParent: destinationParent, Destination: parentIdentity,
		Selected: append([]string(nil), selected...), Phase: ExportCopying,
	}
	if err := validateExportJournal(journal); err != nil {
		return ExportJournal{}, err
	}
	if err := createExportJournal(stateRoot, journal); err != nil {
		return ExportJournal{}, fmt.Errorf("persist export transaction before reservation: %w", err)
	}
	record.Pending = &Pending{Kind: "export-snapshot", ID: id}
	if err := saveRecordTransition(stateRoot, domainID, record, mutationBeginExportSnapshot, nil); err != nil {
		return ExportJournal{}, fmt.Errorf("persist export Pending marker: %w", err)
	}
	root, err := openStateRoot(stateRoot)
	if err != nil {
		return ExportJournal{}, err
	}
	defer root.Close()
	exports, err := openChild(root, "exports", false)
	if err != nil {
		return ExportJournal{}, err
	}
	defer exports.Close()
	transaction, err := openChild(exports, id, true)
	if err != nil {
		return ExportJournal{}, err
	}
	defer transaction.Close()
	copyContext, cancel := context.WithTimeout(ctx, exportCopyDeadline)
	defer cancel()
	var snapshot ExportSnapshot
	if err := diskreserve.Run(copyContext, []string{stateRoot, destinationParent}, func(guarded context.Context) error {
		var copyErr error
		snapshot, copyErr = copier(guarded, stateRoot, request, source, transaction, journal)
		return copyErr
	}); err != nil {
		return ExportJournal{}, fmt.Errorf("copy export snapshot: %w", err)
	}
	ready := journal
	ready.Phase = ExportSnapshotReady
	ready.Snapshot = &snapshot
	if err := advanceExportJournal(ctx, stateRoot, journal, ready); err != nil {
		return ExportJournal{}, fmt.Errorf("publish durable snapshot identity: %w", err)
	}
	if err := finishExportSnapshotPending(stateRoot, domainID, ready); err != nil {
		return ExportJournal{}, fmt.Errorf("clear exact export Pending after snapshot: %w", err)
	}
	return ready, nil
}

func finishExportSnapshotPending(stateRoot string, domainID domain.ID, ready ExportJournal) error {
	if ready.Domain != domainID || ready.Phase != ExportSnapshotReady || ready.Snapshot == nil {
		return fmt.Errorf("export snapshot is not ready to release its volume")
	}
	persisted, err := loadExportJournal(stateRoot, domainID, ready.ID)
	if err != nil || !reflect.DeepEqual(persisted, ready) {
		return fmt.Errorf("durable export snapshot differs before Pending release: %v", err)
	}
	current, err := LoadRecord(stateRoot, domainID, ready.VolumeID)
	if err != nil {
		return err
	}
	if current.Pending == nil || current.Pending.Kind != "export-snapshot" || current.Pending.ID != ready.ID ||
		current.State != StateAvailable || current.Disk == nil || *current.Disk != ready.Source || current.Attachment == nil ||
		current.Attachment.SessionID != ready.SessionID || current.Attachment.SessionName != ready.SessionName || current.Use != nil {
		return fmt.Errorf("export Pending does not match exact ready snapshot")
	}
	current.Pending = nil
	return saveRecordTransitionExpectedPending(stateRoot, domainID, current, mutationFinishExportSnapshot, ready.ID, nil)
}

func copyExportSnapshot(ctx context.Context, stateRoot string, request workspaceformat.Request, source *os.File, dir *os.Root, journal ExportJournal) (ExportSnapshot, error) {
	file, err := dir.OpenFile("snapshot.raw", os.O_RDWR|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return ExportSnapshot{}, err
	}
	defer func() { _ = file.Close() }()
	if _, err := source.Seek(0, io.SeekStart); err != nil {
		return ExportSnapshot{}, err
	}
	copyHash := sha256.New()
	buffer := make([]byte, exportCopyBufferBytes)
	var copied int64
	for copied < journal.SizeBytes {
		if err := ctx.Err(); err != nil {
			return ExportSnapshot{}, err
		}
		chunk := buffer
		if int64(len(chunk)) > journal.SizeBytes-copied {
			chunk = chunk[:journal.SizeBytes-copied]
		}
		count, err := io.ReadFull(source, chunk)
		if err != nil {
			return ExportSnapshot{}, err
		}
		written, err := file.Write(chunk[:count])
		if err != nil {
			return ExportSnapshot{}, err
		}
		if written != count {
			return ExportSnapshot{}, io.ErrShortWrite
		}
		_, _ = copyHash.Write(chunk[:count])
		copied += int64(count)
	}
	if err := file.Sync(); err != nil {
		return ExportSnapshot{}, err
	}
	copyInfo, err := file.Stat()
	if err != nil || copyInfo.Size() != journal.SizeBytes {
		return ExportSnapshot{}, fmt.Errorf("export snapshot length changed: %v", err)
	}
	copyIdentity, err := diskIdentity(copyInfo)
	if err != nil || copyIdentity == journal.Source {
		return ExportSnapshot{}, fmt.Errorf("export snapshot is not a distinct inode: %v", err)
	}
	pathInfo, err := dir.Lstat("snapshot.raw")
	if err != nil || !os.SameFile(copyInfo, pathInfo) || privateRegular(pathInfo) != nil {
		return ExportSnapshot{}, fmt.Errorf("export snapshot path or metadata changed: %v", err)
	}
	if err := checkPrivateACL(filepath.Join(dir.Name(), "snapshot.raw"), pathInfo); err != nil {
		return ExportSnapshot{}, err
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return ExportSnapshot{}, err
	}
	snapshotHash, err := hashExactExportBytes(ctx, file, journal.SizeBytes)
	if err != nil || !equalDigest(snapshotHash, copyHash.Sum(nil)) {
		return ExportSnapshot{}, fmt.Errorf("export snapshot digest differs from copied bytes: %v", err)
	}
	if err := verifyExportSource(ctx, stateRoot, request, source, journal.Source, copyHash.Sum(nil)); err != nil {
		return ExportSnapshot{}, err
	}
	finalInfo, err := dir.Lstat("snapshot.raw")
	if err != nil || !os.SameFile(copyInfo, finalInfo) || finalInfo.Size() != journal.SizeBytes || privateRegular(finalInfo) != nil {
		return ExportSnapshot{}, fmt.Errorf("export snapshot path changed before publication: %v", err)
	}
	finalOpened, err := file.Stat()
	if err != nil || !os.SameFile(finalOpened, finalInfo) || finalOpened.Size() != journal.SizeBytes {
		return ExportSnapshot{}, fmt.Errorf("export snapshot descriptor changed before publication: %v", err)
	}
	if err := file.Close(); err != nil {
		return ExportSnapshot{}, err
	}
	if err := syncDirectory(dir); err != nil {
		return ExportSnapshot{}, err
	}
	return ExportSnapshot{Identity: copyIdentity, SHA256: hex.EncodeToString(copyHash.Sum(nil))}, nil
}

func verifyExportSource(ctx context.Context, stateRoot string, request workspaceformat.Request, source *os.File, expected DiskIdentity, copiedSHA []byte) error {
	readmit, qualified, err := workspaceformat.Admit(stateRoot, request)
	if err != nil {
		return err
	}
	defer readmit.Close()
	if qualified.Identity.Device != expected.Device || qualified.Identity.Inode != expected.Inode {
		return fmt.Errorf("export source identity changed")
	}
	oldInfo, err := source.Stat()
	if err != nil {
		return err
	}
	newInfo, err := readmit.Stat()
	if err != nil || !os.SameFile(oldInfo, newInfo) {
		return fmt.Errorf("export source path changed: %v", err)
	}
	if _, err := source.Seek(0, io.SeekStart); err != nil {
		return err
	}
	hash, err := hashExactExportBytes(ctx, source, request.SizeBytes)
	if err != nil {
		return err
	}
	if !equalDigest(hash, copiedSHA) {
		return fmt.Errorf("export source bytes changed during copy")
	}
	oldInfo, err = source.Stat()
	if err != nil || oldInfo.Size() != request.SizeBytes {
		return fmt.Errorf("export source size changed after digest: %v", err)
	}
	newInfo, err = readmit.Stat()
	if err != nil || !os.SameFile(oldInfo, newInfo) || newInfo.Size() != request.SizeBytes {
		return fmt.Errorf("export source path changed after digest: %v", err)
	}
	return nil
}

func hashExactExportBytes(ctx context.Context, file *os.File, size int64) ([]byte, error) {
	hash := sha256.New()
	buffer := make([]byte, exportCopyBufferBytes)
	for remaining := size; remaining > 0; {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		chunk := buffer
		if int64(len(chunk)) > remaining {
			chunk = chunk[:remaining]
		}
		if _, err := io.ReadFull(file, chunk); err != nil {
			return nil, err
		}
		_, _ = hash.Write(chunk)
		remaining -= int64(len(chunk))
	}
	return hash.Sum(nil), nil
}

func equalDigest(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func admitExportParent(path string) (*os.File, DiskIdentity, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, DiskIdentity{}, fmt.Errorf("export destination parent must be a clean absolute path")
	}
	before, err := os.Lstat(path)
	if err != nil || privateDirectory(before) != nil {
		return nil, DiskIdentity{}, fmt.Errorf("export destination parent is not private: %v", err)
	}
	if err := checkPrivateACL(path, before); err != nil {
		return nil, DiskIdentity{}, err
	}
	dir, err := os.OpenFile(path, os.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, DiskIdentity{}, err
	}
	opened, err := dir.Stat()
	if err != nil || !os.SameFile(before, opened) {
		dir.Close()
		return nil, DiskIdentity{}, fmt.Errorf("export destination parent changed: %v", err)
	}
	names, err := dir.Readdirnames(1)
	if err != nil && !errors.Is(err, io.EOF) || len(names) != 0 {
		dir.Close()
		return nil, DiskIdentity{}, fmt.Errorf("export destination parent must be empty: %v", err)
	}
	identity, err := diskIdentity(opened)
	if err != nil {
		dir.Close()
		return nil, DiskIdentity{}, err
	}
	return dir, identity, nil
}

func checkExportHeadroom(directories ...*os.File) error {
	for _, dir := range directories {
		var stat syscall.Statfs_t
		if err := syscall.Fstatfs(int(dir.Fd()), &stat); err != nil {
			return err
		}
		if stat.Bsize <= 0 || uint64(stat.Blocks) > ^uint64(0)/uint64(stat.Bsize) || uint64(stat.Bavail) > ^uint64(0)/uint64(stat.Bsize) {
			return fmt.Errorf("invalid export filesystem capacity")
		}
		capacity := uint64(stat.Blocks) * uint64(stat.Bsize)
		free := uint64(stat.Bavail) * uint64(stat.Bsize)
		floor := capacity / 10
		if floor < 20<<30 {
			floor = 20 << 30
		}
		if free <= floor+3<<30 {
			return fmt.Errorf("insufficient host headroom for bounded export")
		}
	}
	return nil
}

func newExportUUID() (string, error) {
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", err
	}
	random[6] = random[6]&0x0f | 0x40
	random[8] = random[8]&0x3f | 0x80
	raw := hex.EncodeToString(random[:])
	return raw[:8] + "-" + raw[8:12] + "-" + raw[12:16] + "-" + raw[16:20] + "-" + raw[20:], nil
}
