//go:build linux && amd64

package guestproto

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// This fails if the amd64 implementation loses the Linux renameat2 syscall
// number required for atomic RENAME_NOREPLACE publication.
func TestLinuxAMD64Renameat2SyscallNumber(t *testing.T) {
	if linuxRenameat2Syscall != 316 {
		t.Fatalf("renameat2 syscall = %d, want 316", linuxRenameat2Syscall)
	}
}

// This fails if amd64 falls back to replacement semantics: a new active tree
// must publish atomically, but an existing target must remain untouched.
func TestLinuxAMD64RenameWithoutReplacementFilesystemSemantics(t *testing.T) {
	t.Run("publishes absent target", func(t *testing.T) {
		dir := t.TempDir()
		source, destination := filepath.Join(dir, "source"), filepath.Join(dir, "destination")
		if err := os.WriteFile(source, []byte("new binding"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := renameWithoutReplacement(source, destination); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(source); !os.IsNotExist(err) {
			t.Fatalf("source remains after publish: %v", err)
		}
		contents, err := os.ReadFile(destination)
		if err != nil || string(contents) != "new binding" {
			t.Fatalf("destination = %q, %v", contents, err)
		}
	})
	t.Run("refuses existing target", func(t *testing.T) {
		dir := t.TempDir()
		source, destination := filepath.Join(dir, "source"), filepath.Join(dir, "destination")
		if err := os.WriteFile(source, []byte("new binding"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(destination, []byte("existing binding"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := renameWithoutReplacement(source, destination); !errors.Is(err, syscall.EEXIST) {
			t.Fatalf("rename error = %v, want EEXIST", err)
		}
		for path, want := range map[string]string{source: "new binding", destination: "existing binding"} {
			contents, err := os.ReadFile(path)
			if err != nil || string(contents) != want {
				t.Fatalf("%s = %q, %v", path, contents, err)
			}
		}
	})
}
