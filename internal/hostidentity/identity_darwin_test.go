//go:build darwin && cgo

package hostidentity

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestObservePinnedAPFSFileHasPersistentVolumeAndObjectID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "disk.raw")
	if err := os.WriteFile(path, []byte("synthetic"), 0o600); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	identity, err := Observe(file)
	if err != nil {
		t.Fatal(err)
	}
	info, err := file.Stat()
	if err != nil {
		t.Fatal(err)
	}
	if identity.VolumeUUID == "" || identity.FileID == 0 || identity.FileID != uint64(info.Sys().(*syscall.Stat_t).Ino) {
		t.Fatalf("incomplete pinned identity: %+v", identity)
	}
}
