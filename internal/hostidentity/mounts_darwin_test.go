//go:build darwin && cgo

package hostidentity

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestMountedUUIDIdentityRejectsDistinctFilesystemAndAllowsAlias(t *testing.T) {
	expected := "00112233-4455-6677-8899-aabbccddeeff"
	active := syscall.Fsid{Val: [2]int32{1, 2}}
	alias := mountedVolume{UUID: expected, FSID: active}
	other := mountedVolume{UUID: expected, FSID: syscall.Fsid{Val: [2]int32{3, 4}}}
	if err := requireUniqueMountedUUID(expected, active, []mountedVolume{alias, alias}); err != nil {
		t.Fatalf("same mounted filesystem alias rejected: %v", err)
	}
	if err := requireUniqueMountedUUID(expected, active, []mountedVolume{alias, other}); err == nil {
		t.Fatal("distinct filesystem with duplicate APFS UUID accepted")
	}
	if err := requireUniqueMountedUUID(expected, active, []mountedVolume{{UUID: "other", FSID: other.FSID}}); err == nil {
		t.Fatal("active APFS volume missing from mounted inventory accepted")
	}
}

func TestMountedPathRaceDoesNotAttributeUUIDToOldFSID(t *testing.T) {
	old := syscall.Statfs_t{Fsid: syscall.Fsid{Val: [2]int32{1, 2}}}
	copy(old.Fstypename[:], []int8{'a', 'p', 'f', 's'})
	newMount := old
	newMount.Fsid = syscall.Fsid{Val: [2]int32{3, 4}}
	if sameMountedFilesystem(old, newMount) {
		t.Fatal("remounted path attributed to old filesystem")
	}
	if !sameMountedFilesystem(old, old) {
		t.Fatal("same pinned filesystem rejected")
	}
}

func TestMountedMetadataHandleKeepsIdentityWithoutDirectoryRead(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "reference.txt"), []byte("synthetic"), 0600); err != nil {
		t.Fatal(err)
	}
	file, err := openMountedVolumeMetadata(root)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if _, err := file.Stat(); err != nil {
		t.Fatalf("metadata unavailable: %v", err)
	}
	if _, err := Observe(file); err != nil {
		t.Fatalf("pinned APFS identity unavailable: %v", err)
	}
	if names, err := file.Readdirnames(1); err == nil || len(names) != 0 {
		t.Fatalf("metadata handle allowed directory contents: %v,%v", names, err)
	}
}

func TestMountedMetadataHandleRejectsSymlinkAndNonDirectory(t *testing.T) {
	root := t.TempDir()
	regular := filepath.Join(root, "reference.txt")
	if err := os.WriteFile(regular, []byte("synthetic"), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(root, link); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{link, regular} {
		if file, err := openMountedVolumeMetadata(path); err == nil {
			file.Close()
			t.Fatalf("unsafe metadata target admitted: %s", path)
		}
	}
}
