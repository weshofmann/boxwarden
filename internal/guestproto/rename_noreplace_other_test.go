//go:build !linux

package guestproto

import (
	"os"
	"path/filepath"
	"testing"
)

// This fails if a non-Linux build reintroduces a check-then-rename fallback.
func TestNonLinuxRenameFailsClosed(t *testing.T) {
	dir := t.TempDir()
	source, destination := filepath.Join(dir, "source"), filepath.Join(dir, "destination")
	if err := os.Mkdir(source, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := renameWithoutReplacement(source, destination); err == nil {
		t.Fatal("non-Linux rename fallback accepted")
	}
	if _, err := os.Stat(source); err != nil {
		t.Fatalf("source moved despite refusal: %v", err)
	}
}
