package sshx

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/weshofmann/boxwarden/internal/importx"
)

const importTestID = "039179af-8411-4790-9587-890922080236"

func sftpSnapshotFixture(t *testing.T) (string, importx.Snapshot) {
	t.Helper()
	source := privateRoot(t)
	parent := privateRoot(t)
	if err := os.Mkdir(filepath.Join(source, "nested"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "nested", "project.txt"), []byte("synthetic\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	snapshot, err := importx.CaptureSource(context.Background(), source, parent, importTestID)
	if err != nil {
		t.Fatal(err)
	}
	return parent, snapshot
}

func TestSFTPImportRequiresMeasuredReadback(t *testing.T) {
	parent, snapshot := sftpSnapshotFixture(t)
	connection := testConnection(t)
	runner := &fakeRunner{onRun: func(Command) Result { return Result{} }}
	client := newSFTPClient(runner)
	client.readback = func(context.Context, Connection, []importx.Entry, string, string) error { return nil }
	if _, err := client.TransferImport(context.Background(), connection, parent, importTestID, snapshot.Digest, "/home/boxwarden/workspaces/project"); err == nil {
		t.Fatal("SFTP success without host readback was accepted")
	}
	if len(runner.commands) != 1 {
		t.Fatalf("SFTP command count = %d", len(runner.commands))
	}
	for _, command := range runner.commands {
		if command.Path != sftpPath || strings.Contains(string(command.Stdin), "\n!") || !strings.Contains(string(command.Stdin), "boxwarden-import-"+importTestID) {
			t.Fatalf("unsafe SFTP command: %#v", command)
		}
	}
}

func TestSFTPImportReadbackMatchesCapturedSource(t *testing.T) {
	parent, snapshot := sftpSnapshotFixture(t)
	connection := testConnection(t)
	for _, test := range []struct {
		name    string
		content string
		wantErr bool
	}{
		{"matching", "synthetic\n", false},
		{"different", "changed\n", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			runner := &fakeRunner{onRun: func(Command) Result { return Result{} }}
			client := newSFTPClient(runner)
			client.readback = func(_ context.Context, _ Connection, entries []importx.Entry, _, local string) error {
				for _, entry := range entries {
					if entry.Kind == "file" {
						if err := os.WriteFile(filepath.Join(local, filepath.FromSlash(entry.Path)), []byte(test.content), 0o600); err != nil {
							return err
						}
					}
				}
				return nil
			}
			receipt, err := client.TransferImport(context.Background(), connection, parent, importTestID, snapshot.Digest, "/home/boxwarden/workspaces/project")
			if (err != nil) != test.wantErr {
				t.Fatalf("receipt = %#v, err = %v", receipt, err)
			}
			if !test.wantErr && (receipt.Digest != snapshot.Digest || receipt.FileCount != snapshot.FileCount || receipt.TotalBytes != snapshot.TotalBytes) {
				t.Fatalf("wrong host readback receipt %#v", receipt)
			}
		})
	}
}

func TestSFTPImportRejectsUnsafePathsAndChangedPin(t *testing.T) {
	parent, snapshot := sftpSnapshotFixture(t)
	connection := testConnection(t)
	runner := &fakeRunner{onRun: func(Command) Result { return Result{} }}
	client := newSFTPClient(runner)
	badSnapshot := snapshot
	badSnapshot.Directory = filepath.Join(parent, "bad$path")
	if _, err := importUploadBatch(badSnapshot, "/home/boxwarden/workspaces/project/boxwarden-import-"+importTestID); err == nil {
		t.Fatal("unsafe local SFTP batch path was accepted")
	}
	if err := os.WriteFile(connection.KnownHostsFile, []byte("wrong host key\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := client.TransferImport(context.Background(), connection, parent, importTestID, snapshot.Digest, "/home/boxwarden/workspaces/project"); err == nil || len(runner.commands) != 0 {
		t.Fatalf("changed host-key pin reached SFTP: %v, %d commands", err, len(runner.commands))
	}
}

func TestSFTPImportBatchUsesQualifiedMacSyntax(t *testing.T) {
	_, snapshot := sftpSnapshotFixture(t)
	upload, err := importUploadBatch(snapshot, "/home/boxwarden/workspaces/project/boxwarden-import-"+importTestID)
	if err != nil {
		t.Fatal(err)
	}
	batch := string(upload)
	if strings.Contains(batch, "mkdir -p") || strings.Contains(batch, "get -f ") || strings.Count(batch, "-mkdir ") != 2 || strings.Count(batch, "\ncd ") != 2 || strings.Count(batch, "put -f ") != 1 {
		t.Fatalf("unsupported or non-checked SFTP mkdir syntax: %q", batch)
	}
}
