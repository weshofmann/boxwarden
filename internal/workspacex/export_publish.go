package workspacex

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"github.com/weshofmann/boxwarden/internal/domain"
	"github.com/weshofmann/boxwarden/internal/exportx"
	"github.com/weshofmann/boxwarden/internal/lock"
)

// PublishCapturedExport consumes and removes a complete private inspector
// spool. The exact journal lock covers re-admission, inspected intent, receiver
// publication, and the final journal phase. A failure after receiver rename
// returns the exact final path with an error and leaves inspected state for
// explicit recovery; a retry may never overwrite that path.
func PublishCapturedExport(ctx context.Context, stateRoot string, domainID domain.ID, transactionID string, captured exportx.CapturedInspectorStream) (string, error) {
	return publishCapturedExport(ctx, stateRoot, domainID, transactionID, captured, checkExportHeadroom, exportReceiverReserve)
}

func publishCapturedExport(ctx context.Context, stateRoot string, domainID domain.ID, transactionID string, captured exportx.CapturedInspectorStream, headroom exportHeadroomChecker, reserve func(*os.File) (uint64, error)) (publishedPath string, err error) {
	if headroom == nil || reserve == nil {
		return "", fmt.Errorf("missing export capacity check")
	}
	if captured.Stream == nil {
		return "", fmt.Errorf("export publication requires a captured private stream")
	}
	defer func() { err = errors.Join(err, captured.Remove()) }()
	if _, parseErr := domain.Parse(string(domainID)); parseErr != nil || !validUUID(transactionID) {
		return "", fmt.Errorf("invalid export publication identity: %v", parseErr)
	}
	held, err := lock.Acquire(ctx, stateRoot, "export-"+string(domainID)+"-"+transactionID)
	if err != nil {
		return "", err
	}
	defer func() { err = errors.Join(err, held.Release()) }()
	journal, err := loadExportJournal(stateRoot, domainID, transactionID)
	if err != nil || journal.Phase != ExportSnapshotReady && journal.Phase != ExportInspected {
		return "", fmt.Errorf("export journal is not ready for publication or safe retry: %v", err)
	}
	spoolParent := filepath.Join(stateRoot, filepath.Dir(journal.SnapshotPath))
	if err := captured.AdmitForPublication(spoolParent); err != nil {
		return "", fmt.Errorf("re-admit exact inspector spool: %w", err)
	}
	if err := admitExactExportSnapshot(ctx, stateRoot, journal); err != nil {
		return "", fmt.Errorf("re-admit snapshot before publication: %w", err)
	}
	parent, identity, err := admitExportParent(journal.DestinationParent)
	if err != nil {
		return "", err
	}
	defer parent.Close()
	if identity != journal.Destination {
		return "", fmt.Errorf("export destination parent identity changed")
	}
	if err := headroom(parent); err != nil {
		return "", err
	}
	minimumFree, err := reserve(parent)
	if err != nil {
		return "", err
	}
	var transaction [16]byte
	if _, err := hex.Decode(transaction[:], []byte(exportTransactionHex(journal.ID))); err != nil {
		return "", fmt.Errorf("invalid export transaction bytes: %w", err)
	}
	inspected := journal
	if journal.Phase == ExportSnapshotReady {
		inspected.Phase = ExportInspected
		if err := advanceExportJournalLocked(stateRoot, journal, inspected); err != nil {
			return "", err
		}
	}
	options := exportx.Options{Parent: journal.DestinationParent, TransactionID: transaction,
		Selected: append([]string(nil), journal.Selected...), MaxChunkBytes: 1 << 20,
		MaxFileBytes: 256 << 20, MaxTotalBytes: 256 << 20, MaxFiles: 4096,
		MaxDirectories: 4096, MinFreeBytes: minimumFree}
	publishedPath, err = exportx.ReceiveSelectedExport(ctx, captured.Stream, options)
	if err != nil {
		return publishedPath, err
	}
	if publishedPath != filepath.Join(journal.DestinationParent, exportTransactionHex(journal.ID)) {
		return publishedPath, fmt.Errorf("receiver published an unexpected export path")
	}
	if err := recheckPublishedExportParent(parent, journal); err != nil {
		return publishedPath, err
	}
	published := inspected
	published.Phase = ExportPublished
	if err := advanceExportJournalLocked(stateRoot, inspected, published); err != nil {
		return publishedPath, fmt.Errorf("export output exists but published phase was not durable: %w", err)
	}
	return publishedPath, nil
}

func exportTransactionHex(id string) string {
	return id[:8] + id[9:13] + id[14:18] + id[19:23] + id[24:]
}

func exportReceiverReserve(parent *os.File) (uint64, error) {
	var stat syscall.Statfs_t
	if err := syscall.Fstatfs(int(parent.Fd()), &stat); err != nil {
		return 0, err
	}
	if stat.Bsize <= 0 || uint64(stat.Blocks) > ^uint64(0)/uint64(stat.Bsize) {
		return 0, fmt.Errorf("invalid export destination capacity")
	}
	capacity := uint64(stat.Blocks) * uint64(stat.Bsize)
	floor := capacity / 10
	if floor < 20<<30 {
		floor = 20 << 30
	}
	return floor + (1 << 30), nil
}

func recheckPublishedExportParent(opened *os.File, journal ExportJournal) error {
	pathInfo, err := os.Lstat(journal.DestinationParent)
	if err != nil || privateDirectory(pathInfo) != nil {
		return fmt.Errorf("published export parent is no longer private: %v", err)
	}
	if err := checkPrivateACL(journal.DestinationParent, pathInfo); err != nil {
		return err
	}
	openedInfo, err := opened.Stat()
	if err != nil || !os.SameFile(pathInfo, openedInfo) {
		return fmt.Errorf("published export parent path changed: %v", err)
	}
	identity, err := diskIdentity(pathInfo)
	if err != nil || identity != journal.Destination {
		return fmt.Errorf("published export parent identity changed: %v", err)
	}
	return nil
}
