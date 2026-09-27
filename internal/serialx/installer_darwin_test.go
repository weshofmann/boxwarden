//go:build darwin && cgo

package serialx

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestInstallerRuntimeOwnsExactPrivateSerialSubtree(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := CreateInstallerRuntime(context.Background(), dir, "run-3"); err == nil {
		t.Fatal("invalid run ID created serial state")
	}
	serialPath := filepath.Join(dir, "serial")
	if _, err := os.Lstat(serialPath); !os.IsNotExist(err) {
		t.Fatalf("invalid run ID created serial state: %v", err)
	}
	runtime, err := CreateInstallerRuntime(context.Background(), dir, "run-0123456789ab")
	if err != nil {
		t.Fatal(err)
	}
	if runtime.TartSlave() != filepath.Join(serialPath, "tart-serial") {
		t.Fatalf("installer endpoint = %q", runtime.TartSlave())
	}
	if _, err := CreateInstallerRuntime(context.Background(), dir, "run-0123456789ab"); err == nil {
		t.Fatal("second serial runtime adopted owned subtree")
	}
	if err := runtime.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(serialPath); !os.IsNotExist(err) {
		t.Fatalf("serial subtree retained after owner close: %v", err)
	}
}
