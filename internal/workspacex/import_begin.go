package workspacex

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/weshofmann/boxwarden/internal/domain"
	"github.com/weshofmann/boxwarden/internal/importx"
	"github.com/weshofmann/boxwarden/internal/lock"
	"github.com/weshofmann/boxwarden/internal/session"
	"github.com/weshofmann/boxwarden/internal/supervisor"
)

const maxImportSnapshotAge = 5 * time.Second

// ImportReadinessReader obtains a fresh exact-generation snapshot from the
// retained supervisor. The durable session readiness bit is only an audit hint.
type ImportReadinessReader interface {
	Snapshot(context.Context, supervisor.Binding) (supervisor.Snapshot, error)
}

// BeginImport binds a captured host snapshot to one live, mount-probed clean
// session and its exact active workspace Use. It holds the transition lock
// across admission so stop and rebuild cannot race the journal reservation.
func BeginImport(ctx context.Context, stateRoot string, domainID domain.ID, sessionName, volumeID, transactionID string, reader ImportReadinessReader) (result ImportJournal, err error) {
	if _, parseErr := domain.Parse(string(domainID)); parseErr != nil || !validSessionName(sessionName) || !validUUID(volumeID) || !validUUID(transactionID) || reader == nil {
		return ImportJournal{}, fmt.Errorf("invalid import request or readiness reader")
	}
	transition, err := lock.Acquire(ctx, stateRoot, "transition-"+string(domainID)+"-"+sessionName)
	if err != nil {
		return ImportJournal{}, err
	}
	defer func() { err = errors.Join(err, transition.Release()) }()
	sessionLock, err := lock.AcquireSession(ctx, stateRoot, string(domainID), sessionName)
	if err != nil {
		return ImportJournal{}, err
	}
	defer func() { err = errors.Join(err, sessionLock.Release()) }()
	storageLock, err := AcquireStorageOperation(ctx, stateRoot, domainID)
	if err != nil {
		return ImportJournal{}, err
	}
	defer func() { err = errors.Join(err, storageLock.Release()) }()
	current, err := session.LoadRecord(stateRoot, string(domainID), sessionName)
	if err != nil {
		return ImportJournal{}, err
	}
	if current.Domain != domainID || current.Version != 2 || current.Mode != session.ModeClean ||
		current.IntendedState != session.StateRunning || current.Readiness.Status != session.ReadinessReady ||
		current.Backend.Kind != "tart" || !validUUID(current.ID) || !validUUID(current.StartGeneration) {
		return ImportJournal{}, fmt.Errorf("import requires exact clean running session with durable readiness")
	}
	if err := session.RequireNoRebuild(stateRoot, domainID, sessionName); err != nil {
		return ImportJournal{}, err
	}
	attached, err := listSessionAttachments(ctx, stateRoot, domainID, current.ID, sessionName)
	if err != nil {
		return ImportJournal{}, err
	}
	var selected *Record
	for i := range attached {
		if attached[i].VolumeID == volumeID {
			selected = &attached[i]
			break
		}
	}
	if selected == nil || selected.State != StateAvailable || selected.Disk == nil || selected.Pending != nil || selected.Attachment == nil || selected.Use == nil ||
		selected.Attachment.SessionID != current.ID || selected.Attachment.SessionName != sessionName ||
		*selected.Use != (Use{BackendKind: "tart", BackendObject: current.Backend.ObjectID, Generation: current.StartGeneration}) {
		return ImportJournal{}, fmt.Errorf("import workspace lacks exact attached live Use")
	}
	binding := supervisor.Binding{Domain: string(domainID), SessionID: current.ID, BackendKind: current.Backend.Kind,
		BackendObject: current.Backend.ObjectID, Generation: current.StartGeneration}
	live, err := reader.Snapshot(ctx, binding)
	if err != nil {
		return ImportJournal{}, fmt.Errorf("fresh import readiness: %w", err)
	}
	now := time.Now()
	if live.Binding != binding || live.ObservedAt.IsZero() || live.ObservedAt.After(now) || now.Sub(live.ObservedAt) > maxImportSnapshotAge ||
		!live.BackendRunning || !live.SerialHealthy || !live.PinPresent || !live.CertificateCurrent || !live.ProbeOK || !live.ZoneMatches {
		return ImportJournal{}, fmt.Errorf("exact live session and workspace mount readiness is unproven")
	}
	if err := ctx.Err(); err != nil {
		return ImportJournal{}, err
	}
	snapshot, err := importx.InspectSnapshot(filepath.Join(stateRoot, "imports"), transactionID)
	if err != nil {
		return ImportJournal{}, fmt.Errorf("admit captured host source: %w", err)
	}
	result = ImportJournal{Version: 1, ID: transactionID, Domain: domainID, SessionID: current.ID, SessionName: sessionName,
		BackendObject: current.Backend.ObjectID, Generation: current.StartGeneration, VolumeID: selected.VolumeID,
		FilesystemUUID: selected.FilesystemUUID, MountPath: selected.Attachment.MountPath,
		SourceDigest: snapshot.Digest, FileCount: snapshot.FileCount, TotalBytes: snapshot.TotalBytes, Phase: ImportCaptured}
	if err := createImportJournal(stateRoot, result); err != nil {
		return result, err // Exact journal may be visible after an uncertain parent sync.
	}
	return result, nil
}
