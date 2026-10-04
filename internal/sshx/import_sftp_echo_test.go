package sshx

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/weshofmann/boxwarden/internal/importx"
)

// Keep NewSFTPClient's actual bounded exec runner. Only replace the remote SSH
// endpoint with the installed local SFTP server, without networking or a VM.
type localImportSFTPRunner struct {
	delegate Runner
	result   Result
}

func (runner *localImportSFTPRunner) Run(ctx context.Context, command Command) (Result, error) {
	command.Args = []string{"-q", "-b", "-", "-D", "/usr/libexec/sftp-server"}
	result, err := runner.delegate.Run(ctx, command)
	runner.result = result
	return result, err
}

func localImportSFTPClient(t *testing.T) (*SFTPClient, *localImportSFTPRunner) {
	t.Helper()
	if runtime.GOOS != "darwin" {
		t.Skip("qualified local OpenSSH server path is macOS-specific")
	}
	if _, err := os.Stat("/usr/libexec/sftp-server"); err != nil {
		t.Skipf("local OpenSSH SFTP server unavailable: %v", err)
	}
	client := NewSFTPClient()
	runner := &localImportSFTPRunner{delegate: client.runner}
	client.runner = runner
	return client, runner
}

func TestSFTP4096FileUploadKeepsProductionDiagnosticsBounded(t *testing.T) {
	client, runner := localImportSFTPClient(t)
	source, guest := privateRoot(t), privateRoot(t)
	snapshot := importx.Snapshot{Directory: source}
	for i := 0; i < 4096; i++ {
		name := fmt.Sprintf("file%04d.txt", i)
		if err := os.WriteFile(filepath.Join(source, name), nil, 0o600); err != nil {
			t.Fatal(err)
		}
		snapshot.Entries = append(snapshot.Entries, importx.Entry{Path: name, Kind: "file"})
	}
	remote := filepath.Join(guest, "import")
	batch, err := importUploadBatch(snapshot, remote)
	if err != nil {
		t.Fatal(err)
	}
	if len(batch) <= 256<<10 {
		t.Fatal("fixture cannot reproduce large command echo")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	connection := testConnection(t)
	for attempt := 0; attempt < 2; attempt++ {
		err := client.runSFTP(ctx, connection, batch)
		entries, readErr := os.ReadDir(remote)
		if readErr != nil || len(entries) != 4096 {
			t.Fatalf("uploaded count=%d, err=%v", len(entries), readErr)
		}
		if err != nil || runner.result.Truncated {
			t.Fatalf("successful upload rejected by diagnostics: err=%v, stdout=%d, stderr=%d, truncated=%t", err, len(runner.result.Stdout), len(runner.result.Stderr), runner.result.Truncated)
		}
		if runner.result.Stdout != "" {
			t.Fatalf("routine command echo retained %d bytes", len(runner.result.Stdout))
		}
	}
}

func TestQuietSFTPBatchStillAbortsAndReportsRequiredCommandFailure(t *testing.T) {
	for _, scenario := range []string{"missing upload", "failed cd"} {
		t.Run(scenario, func(t *testing.T) {
			client, runner := localImportSFTPClient(t)
			source, guest := privateRoot(t), privateRoot(t)
			for _, name := range []string{"first.txt", "after.txt"} {
				if err := os.WriteFile(filepath.Join(source, name), []byte("data"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			remote := filepath.Join(guest, "import")
			if scenario == "missing upload" {
				if err := os.Remove(filepath.Join(source, "first.txt")); err != nil {
					t.Fatal(err)
				}
			} else if err := os.WriteFile(remote, []byte("original"), 0o600); err != nil {
				t.Fatal(err)
			}
			batch, err := importUploadBatch(importx.Snapshot{Directory: source, Entries: []importx.Entry{{Path: "first.txt", Kind: "file", Size: 4}, {Path: "after.txt", Kind: "file", Size: 4}}}, remote)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := client.runSFTP(ctx, testConnection(t), batch); err == nil {
				t.Fatal("required command failure accepted")
			}
			if runner.result.Truncated || runner.result.Stderr == "" {
				t.Fatalf("failure diagnostics missing or truncated: %+v", runner.result)
			}
			if _, err := os.Stat(filepath.Join(remote, "after.txt")); err == nil {
				t.Fatal("batch continued after required command failure")
			}
			if scenario == "failed cd" {
				body, err := os.ReadFile(remote)
				if err != nil || string(body) != "original" {
					t.Fatalf("failed cd changed destination file: %q,%v", body, err)
				}
			}
		})
	}
}
