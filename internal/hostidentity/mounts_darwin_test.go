//go:build darwin && cgo

package hostidentity

import (
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
