package workspacex

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/weshofmann/boxwarden/internal/domain"
	"github.com/weshofmann/boxwarden/internal/lock"
)

const maxInspectorRequestBytes = 128 << 10

// PreparedExportInspectorRequest binds one admitted, private snapshot to the
// guest's exact request envelope. It is an input to bundle creation, not
// authorization to launch or publish: the caller must re-admit the snapshot
// after the helper reaps and verify the exact journal phase and selection.
type PreparedExportInspectorRequest struct {
	Journal      ExportJournal
	SnapshotPath string
	Request      []byte
}

func PrepareExportInspectorRequest(ctx context.Context, stateRoot string, domainID domain.ID, transactionID string) (PreparedExportInspectorRequest, error) {
	if _, err := domain.Parse(string(domainID)); err != nil {
		return PreparedExportInspectorRequest{}, err
	}
	if !validUUID(transactionID) {
		return PreparedExportInspectorRequest{}, fmt.Errorf("invalid export transaction ID")
	}
	held, err := lock.Acquire(ctx, stateRoot, "export-"+string(domainID)+"-"+transactionID)
	if err != nil {
		return PreparedExportInspectorRequest{}, err
	}
	defer held.Release()
	journal, err := loadExportJournal(stateRoot, domainID, transactionID)
	if err != nil {
		return PreparedExportInspectorRequest{}, err
	}
	request, err := encodeExportInspectorRequest(journal)
	if err != nil {
		return PreparedExportInspectorRequest{}, err
	}
	if err := admitExactExportSnapshot(ctx, stateRoot, journal); err != nil {
		return PreparedExportInspectorRequest{}, fmt.Errorf("admit exact export snapshot before inspector request: %w", err)
	}
	return PreparedExportInspectorRequest{
		Journal: journal, SnapshotPath: filepath.Join(stateRoot, journal.SnapshotPath), Request: request,
	}, nil
}

func encodeExportInspectorRequest(journal ExportJournal) ([]byte, error) {
	if err := validateExportJournal(journal); err != nil {
		return nil, err
	}
	if journal.Phase != ExportSnapshotReady {
		return nil, fmt.Errorf("export snapshot is not ready for inspector request")
	}
	raw, err := json.Marshal(struct {
		Version        int      `json:"version"`
		Transaction    string   `json:"transaction"`
		FilesystemUUID string   `json:"filesystem_uuid"`
		DiskBytes      int64    `json:"disk_bytes"`
		Selected       []string `json:"selected"`
	}{
		Version: 1, Transaction: strings.ReplaceAll(journal.ID, "-", ""),
		FilesystemUUID: journal.FilesystemUUID, DiskBytes: journal.SizeBytes,
		Selected: journal.Selected,
	})
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 || len(raw)+1 > maxInspectorRequestBytes {
		return nil, fmt.Errorf("inspector request exceeds private initramfs member bound")
	}
	return append(raw, '\n'), nil
}
