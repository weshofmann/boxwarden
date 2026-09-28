package workspaceformat

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/weshofmann/boxwarden/internal/hostidentity"
)

func TestOpenVolumesRejectsPinnedFilesystemMismatch(t *testing.T) {
	root := testRoot(t)
	if err := os.Mkdir(filepath.Join(root, "volumes"), 0o700); err != nil {
		t.Fatal(err)
	}
	pinned, err := openStateRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer pinned.Close()
	called := false
	volumes, err := openVolumesWithCheck(pinned, false, func(*os.Root, *os.Root) error {
		called = true
		return errors.New("injected nested filesystem")
	})
	if volumes != nil {
		volumes.Close()
	}
	if err == nil || !called {
		t.Fatalf("nested volumes filesystem admitted: called=%v, err=%v", called, err)
	}
}

func TestExactRawFileRejectsPinnedFilesystemMismatch(t *testing.T) {
	root := testRoot(t)
	if err := os.Mkdir(filepath.Join(root, "volumes"), 0o700); err != nil {
		t.Fatal(err)
	}
	volumes, err := os.OpenRoot(filepath.Join(root, "volumes"))
	if err != nil {
		t.Fatal(err)
	}
	defer volumes.Close()
	file, err := volumes.OpenFile(rawName(testVolumeID), os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if err := file.Truncate(4096); err != nil {
		t.Fatal(err)
	}
	called := false
	_, err = exactFileWithCheck(volumes, rawName(testVolumeID), file, 4096, func(*os.Root, *os.File) error {
		called = true
		return errors.New("injected raw-file filesystem mismatch")
	})
	if err == nil || !called {
		t.Fatalf("cross-filesystem raw file admitted: called=%v, err=%v", called, err)
	}
}

func TestV2CreationHostIdentityMustMatchEnrolledUUID(t *testing.T) {
	storage := &hostidentity.StorageExpectation{VolumeUUID: "00112233-4455-6677-8899-aabbccddeeff"}
	identity := hostidentity.Identity{VolumeUUID: "11112233-4455-6677-8899-aabbccddeeff", FileID: 7}
	if err := requireCreationHostIdentity(storage, identity, 7); err == nil {
		t.Fatal("v2 raw file on wrong APFS volume accepted")
	}
	identity.VolumeUUID = storage.VolumeUUID
	if err := requireCreationHostIdentity(storage, identity, 7); err != nil {
		t.Fatal(err)
	}
}
