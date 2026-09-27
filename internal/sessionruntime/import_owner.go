package sessionruntime

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/weshofmann/boxwarden/internal/domain"
	"github.com/weshofmann/boxwarden/internal/session"
	"github.com/weshofmann/boxwarden/internal/sshx"
	"github.com/weshofmann/boxwarden/internal/supervisor"
	"github.com/weshofmann/boxwarden/internal/workspacex"
)

// TransferImport keeps the current management credential inside the retained
// owner. The caller holds the session transition lock for the full RPC; the
// owner independently admits the durable journal, session, and mounted Use.
func (o *Owner) TransferImport(ctx context.Context, spec supervisor.ImportTransfer) (supervisor.ImportResult, error) {
	if o == nil || o.deps.importer == nil {
		return supervisor.ImportResult{}, fmt.Errorf("runtime import transfer is unavailable")
	}
	o.readyMu.Lock()
	defer o.readyMu.Unlock()
	before := o.Snapshot(ctx)
	if !readyImportSnapshot(before) {
		return supervisor.ImportResult{}, fmt.Errorf("exact runtime is not ready for import")
	}
	o.mu.Lock()
	connection, binding, sshBinding, runtimePath := o.connection, o.binding, o.sshBinding, o.runtimePath
	stateRoot, sessionName := o.stateRoot, o.sessionName
	mounts := append([]sshx.WorkspaceMount(nil), o.workspaceMounts...)
	o.mu.Unlock()
	if before.Binding != binding || connection.Binding != sshBinding || connection.Binding.SessionID != binding.SessionID ||
		connection.Binding.BackendObject != binding.BackendObject || connection.RuntimeDirectory != runtimePath {
		return supervisor.ImportResult{}, fmt.Errorf("import connection no longer matches exact runtime")
	}
	if err := admitOwnerImport(stateRoot, sessionName, binding, mounts, spec); err != nil {
		return supervisor.ImportResult{}, err
	}
	receipt, err := o.deps.importer.TransferImport(ctx, connection, filepath.Join(stateRoot, "imports"), spec.TransactionID, spec.SourceDigest, spec.MountPath)
	if err != nil {
		return supervisor.ImportResult{}, err
	}
	after := o.Snapshot(ctx)
	if after.Binding != binding || !readyImportSnapshot(after) {
		return supervisor.ImportResult{}, fmt.Errorf("exact runtime changed during import transfer")
	}
	if err := admitOwnerImport(stateRoot, sessionName, binding, mounts, spec); err != nil {
		return supervisor.ImportResult{}, err
	}
	journal, err := workspacex.LoadImportJournal(stateRoot, domain.ID(binding.Domain), spec.TransactionID)
	if err != nil {
		return supervisor.ImportResult{}, err
	}
	if receipt.Digest != spec.SourceDigest || receipt.FileCount < 1 || receipt.FileCount > 256 ||
		receipt.TotalBytes < 0 || receipt.TotalBytes > 16<<20 || receipt.FileCount != journal.FileCount || receipt.TotalBytes != journal.TotalBytes ||
		receipt.RemotePath != spec.MountPath+"/boxwarden-import-"+spec.TransactionID {
		return supervisor.ImportResult{}, fmt.Errorf("import readback receipt differs from exact transaction")
	}
	return supervisor.ImportResult{Digest: receipt.Digest, FileCount: receipt.FileCount, TotalBytes: receipt.TotalBytes, RemotePath: receipt.RemotePath}, nil
}

func readyImportSnapshot(snapshot supervisor.Snapshot) bool {
	return snapshot.BackendRunning && snapshot.SerialHealthy && snapshot.PinPresent && snapshot.CertificateCurrent && snapshot.ProbeOK && snapshot.ZoneMatches
}

func admitOwnerImport(stateRoot, sessionName string, binding supervisor.Binding, mounts []sshx.WorkspaceMount, spec supervisor.ImportTransfer) error {
	if stateRoot == "" || sessionName == "" || spec.MountPath == "" {
		return fmt.Errorf("incomplete import owner binding")
	}
	domainID, err := domain.Parse(binding.Domain)
	if err != nil {
		return err
	}
	current, err := session.LoadRecord(stateRoot, binding.Domain, sessionName)
	if err != nil {
		return err
	}
	if current.Domain != domainID || current.ID != binding.SessionID || current.Backend.Kind != binding.BackendKind ||
		current.Backend.ObjectID != binding.BackendObject || current.StartGeneration != binding.Generation ||
		current.Mode != session.ModeClean || current.IntendedState != session.StateRunning || current.Readiness.Status != session.ReadinessReady {
		return fmt.Errorf("durable session changed before import transfer")
	}
	if err := session.RequireNoRebuild(stateRoot, domainID, sessionName); err != nil {
		return err
	}
	journal, err := workspacex.LoadImportJournal(stateRoot, domainID, spec.TransactionID)
	if err != nil {
		return err
	}
	if journal.Phase != workspacex.ImportTransferring || journal.SessionID != binding.SessionID || journal.SessionName != sessionName ||
		journal.BackendObject != binding.BackendObject || journal.Generation != binding.Generation || journal.VolumeID != spec.VolumeID ||
		journal.FilesystemUUID != spec.FilesystemUUID || journal.MountPath != spec.MountPath || journal.SourceDigest != spec.SourceDigest {
		return fmt.Errorf("import journal differs from exact runtime and workspace")
	}
	volume, err := workspacex.LoadRecord(stateRoot, domainID, spec.VolumeID)
	if err != nil {
		return err
	}
	if volume.State != workspacex.StateAvailable || volume.Disk == nil || volume.Pending != nil || volume.Attachment == nil || volume.Use == nil ||
		volume.FilesystemUUID != spec.FilesystemUUID || volume.Attachment.SessionID != binding.SessionID ||
		volume.Attachment.SessionName != sessionName || volume.Attachment.MountPath != spec.MountPath ||
		*volume.Use != (workspacex.Use{BackendKind: binding.BackendKind, BackendObject: binding.BackendObject, Generation: binding.Generation}) {
		return fmt.Errorf("import workspace lacks exact active Use")
	}
	for _, mount := range mounts {
		if mount.VolumeID == spec.VolumeID && mount.FilesystemUUID == spec.FilesystemUUID && mount.MountPath == spec.MountPath {
			return nil
		}
	}
	return fmt.Errorf("import workspace was not admitted at current launch")
}
