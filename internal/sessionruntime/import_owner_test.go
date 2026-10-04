package sessionruntime

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/weshofmann/boxwarden/internal/domain"
	"github.com/weshofmann/boxwarden/internal/importx"
	"github.com/weshofmann/boxwarden/internal/session"
	"github.com/weshofmann/boxwarden/internal/sshx"
	"github.com/weshofmann/boxwarden/internal/supervisor"
	"github.com/weshofmann/boxwarden/internal/workspacex"
)

func TestImportOwnerAdmitsExactTransferringWorkspaceGeneration(t *testing.T) {
	f := newFixture(t)
	f.record.IntendedState = session.StateRunning
	f.record.Readiness = session.ReadinessRecord{Status: session.ReadinessReady}
	if err := session.SaveRecord(f.root, domain.ID("work"), f.record); err != nil {
		t.Fatal(err)
	}
	spec := supervisor.ImportTransfer{TransactionID: "039179af-8411-4790-9587-890922080236", SourceDigest: strings.Repeat("a", 64),
		VolumeID: "a7d43c10-344e-4617-812d-c8a1c7253631", FilesystemUUID: "691ec498-707b-47f0-a0f2-1869259068ed", MountPath: "/home/boxwarden/workspaces/project"}
	volume := workspacex.Record{Version: 1, Domain: domain.ID("work"), VolumeID: spec.VolumeID, FilesystemUUID: spec.FilesystemUUID,
		SizeBytes: 16 << 20, Format: workspacex.FormatRawExt4, State: workspacex.StateAvailable,
		Disk:       &workspacex.DiskIdentity{Device: 1, Inode: 2},
		Attachment: &workspacex.Attachment{SessionID: f.record.ID, SessionName: "dev", MountPath: spec.MountPath},
		Use:        &workspacex.Use{BackendKind: "tart", BackendObject: f.record.Backend.ObjectID, Generation: f.record.StartGeneration}}
	journal := workspacex.ImportJournal{Version: 1, ID: spec.TransactionID, Domain: domain.ID("work"), SessionID: f.record.ID,
		SessionName: "dev", BackendObject: f.record.Backend.ObjectID, Generation: f.record.StartGeneration,
		VolumeID: spec.VolumeID, FilesystemUUID: spec.FilesystemUUID, MountPath: spec.MountPath, SourceDigest: spec.SourceDigest,
		FileCount: 1, TotalBytes: 9, Phase: workspacex.ImportTransferring}
	writePrivateImportFixture(t, f.root, "workspaces", spec.VolumeID+".json", volume)
	writePrivateImportFixture(t, f.root, "imports", spec.TransactionID+".json", journal)
	mounts := []sshx.WorkspaceMount{{VolumeID: spec.VolumeID, FilesystemUUID: spec.FilesystemUUID, MountPath: spec.MountPath}}
	if err := admitOwnerImport(f.root, "dev", f.request.Binding, mounts, spec); err != nil {
		t.Fatalf("exact import binding rejected: %v", err)
	}
	wrong := f.request.Binding
	wrong.Generation = "00000000-0000-4000-8000-000000000000"
	if err := admitOwnerImport(f.root, "dev", wrong, mounts, spec); err == nil {
		t.Fatal("foreign generation admitted")
	}
	if err := admitOwnerImport(f.root, "dev", f.request.Binding, nil, spec); err == nil {
		t.Fatal("workspace absent from retained launch admitted")
	}
	volume.Use.Generation = "00000000-0000-4000-8000-000000000000"
	writePrivateImportFixture(t, f.root, "workspaces", spec.VolumeID+".json", volume)
	if err := admitOwnerImport(f.root, "dev", f.request.Binding, mounts, spec); err == nil {
		t.Fatal("changed durable workspace Use admitted")
	}
	volume.Use.Generation = f.record.StartGeneration
	writePrivateImportFixture(t, f.root, "workspaces", spec.VolumeID+".json", volume)
	journal.Phase = workspacex.ImportCaptured
	writePrivateImportFixture(t, f.root, "imports", spec.TransactionID+".json", journal)
	if err := admitOwnerImport(f.root, "dev", f.request.Binding, mounts, spec); err == nil {
		t.Fatal("uncommitted captured journal admitted for transfer")
	}
}

func writePrivateImportFixture(t *testing.T, stateRoot, dir, name string, value any) {
	t.Helper()
	path := filepath.Join(stateRoot, dir)
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, name), append(raw, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
}

// The retained owner must admit the same raised source bounds as capture and
// SFTP, while still binding the receipt to the durable measured transaction.
type ownerImportClient func(context.Context, sshx.Connection, string, string, string, string) (sshx.ImportReceipt, error)

func (f ownerImportClient) TransferImport(ctx context.Context, conn sshx.Connection, parent, id, digest, mount string) (sshx.ImportReceipt, error) {
	return f(ctx, conn, parent, id, digest, mount)
}

func TestImportOwnerLargerMeasuredReceipt(t *testing.T) {
	for _, tc := range []struct {
		name   string
		files  int
		bytes  int64
		mutate func(*sshx.ImportReceipt)
		want   bool
	}{
		{"larger project", 1003, 48383040, nil, true},
		{"source boundaries", importx.MaxFiles, importx.MaxTotalBytes, nil, true},
		{"extra file", importx.MaxFiles, 0, func(r *sshx.ImportReceipt) { r.FileCount++ }, false},
		{"extra byte", 4, importx.MaxTotalBytes, func(r *sshx.ImportReceipt) { r.TotalBytes++ }, false},
		{"different measured count", 1003, 48383040, func(r *sshx.ImportReceipt) { r.FileCount-- }, false},
		{"different digest", 1003, 48383040, func(r *sshx.ImportReceipt) { r.Digest = strings.Repeat("b", 64) }, false},
		{"different remote", 1003, 48383040, func(r *sshx.ImportReceipt) { r.RemotePath += "/other" }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, _ := readyFixture(t)
			for _, op := range []func(context.Context) error{func(ctx context.Context) error { return f.owner.Start(ctx, f.request) }, f.owner.Bootstrap, f.owner.Ready} {
				if err := op(t.Context()); err != nil {
					t.Fatal(err)
				}
			}
			t.Cleanup(func() { _ = f.owner.Stop(context.Background()); _ = f.owner.Wait(context.Background()) })
			f.record.IntendedState = session.StateRunning
			f.record.Readiness = session.ReadinessRecord{Status: session.ReadinessReady}
			if err := session.SaveRecord(f.root, f.record.Domain, f.record); err != nil {
				t.Fatal(err)
			}
			spec := supervisor.ImportTransfer{TransactionID: "039179af-8411-4790-9587-890922080236", SourceDigest: strings.Repeat("a", 64),
				VolumeID: "a7d43c10-344e-4617-812d-c8a1c7253631", FilesystemUUID: "691ec498-707b-47f0-a0f2-1869259068ed", MountPath: "/home/boxwarden/workspaces/project"}
			volume := workspacex.Record{Version: 1, Domain: f.record.Domain, VolumeID: spec.VolumeID, FilesystemUUID: spec.FilesystemUUID,
				SizeBytes: 512 << 20, Format: workspacex.FormatRawExt4, State: workspacex.StateAvailable,
				Disk:       &workspacex.DiskIdentity{Device: 1, Inode: 2},
				Attachment: &workspacex.Attachment{SessionID: f.record.ID, SessionName: string(f.record.Name), MountPath: spec.MountPath},
				Use:        &workspacex.Use{BackendKind: "tart", BackendObject: f.record.Backend.ObjectID, Generation: f.record.StartGeneration}}
			journal := workspacex.ImportJournal{Version: 1, ID: spec.TransactionID, Domain: f.record.Domain, SessionID: f.record.ID,
				SessionName: string(f.record.Name), BackendObject: f.record.Backend.ObjectID, Generation: f.record.StartGeneration,
				VolumeID: spec.VolumeID, FilesystemUUID: spec.FilesystemUUID, MountPath: spec.MountPath, SourceDigest: spec.SourceDigest,
				FileCount: tc.files, TotalBytes: tc.bytes, Phase: workspacex.ImportTransferring}
			writePrivateImportFixture(t, f.root, "workspaces", spec.VolumeID+".json", volume)
			writePrivateImportFixture(t, f.root, "imports", spec.TransactionID+".json", journal)
			f.owner.workspaceMounts = []sshx.WorkspaceMount{{VolumeID: spec.VolumeID, FilesystemUUID: spec.FilesystemUUID, MountPath: spec.MountPath}}
			calls := 0
			f.owner.deps.importer = ownerImportClient(func(_ context.Context, conn sshx.Connection, parent, id, digest, mount string) (sshx.ImportReceipt, error) {
				calls++
				if conn != f.owner.connection || parent != filepath.Join(f.root, "imports") || id != spec.TransactionID || digest != spec.SourceDigest || mount != spec.MountPath {
					t.Fatal("foreign transfer binding")
				}
				r := sshx.ImportReceipt{Digest: digest, FileCount: tc.files, TotalBytes: tc.bytes, RemotePath: mount + "/boxwarden-import-" + id}
				if tc.mutate != nil {
					tc.mutate(&r)
				}
				return r, nil
			})
			result, err := f.owner.TransferImport(t.Context(), spec)
			if (err == nil) != tc.want || calls != 1 {
				t.Fatalf("result=%#v calls=%d err=%v", result, calls, err)
			}
			if tc.want && (result.FileCount != tc.files || result.TotalBytes != tc.bytes) {
				t.Fatalf("changed measured receipt: %#v", result)
			}
		})
	}
}
