//go:build darwin && cgo

package hostidentity

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestStorageAnchorRequiresExpectedMountUUIDAndSeparateFilesystem(t *testing.T) {
	anchor := StorageExpectation{MountPoint: "/Volumes/Qualified", VolumeUUID: "00112233-4455-6677-8899-aabbccddeeff"}
	configFS := syscall.Fsid{Val: [2]int32{1, 2}}
	storageFS := syscall.Fsid{Val: [2]int32{3, 4}}
	if err := validateStorageObservations(anchor, configFS, storageFS, "/Volumes/Qualified", anchor.VolumeUUID); err != nil {
		t.Fatalf("separate exact backing rejected: %v", err)
	}
	if err := validateStorageObservations(anchor, storageFS, storageFS, "/Volumes/Qualified", anchor.VolumeUUID); err == nil {
		t.Fatal("config on backing filesystem accepted")
	}
	if err := validateStorageObservations(anchor, configFS, storageFS, "/Volumes/Other", anchor.VolumeUUID); err == nil {
		t.Fatal("wrong mounted filesystem at configured path accepted")
	}
	if err := validateStorageObservations(anchor, configFS, storageFS, "/Volumes/Qualified", "10213243-5465-4768-899a-bbccddeeff00"); err == nil {
		t.Fatal("wrong APFS volume UUID accepted")
	}
}

func TestStorageAnchorRejectsUnsafeConfigAndRootMetadata(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(base, "state")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	configBase, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(configBase, "config.json")
	if err := os.WriteFile(config, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(config)
	if err != nil || privateStorageFile(info) != nil {
		t.Fatalf("private config refused: %v", err)
	}
	if err := os.Chmod(config, 0o644); err != nil {
		t.Fatal(err)
	}
	info, _ = os.Lstat(config)
	if privateStorageFile(info) == nil {
		t.Fatal("world-readable authority accepted")
	}
	if err := os.Chmod(config, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(config, config+".alias"); err != nil {
		t.Fatal(err)
	}
	info, _ = os.Lstat(config)
	if privateStorageFile(info) == nil {
		t.Fatal("multiply-linked authority accepted")
	}
	rootInfo, _ := os.Lstat(root)
	if err := privateStorageDirectory(rootInfo); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(root, 0o755); err != nil {
		t.Fatal(err)
	}
	rootInfo, _ = os.Lstat(root)
	if privateStorageDirectory(rootInfo) == nil {
		t.Fatal("public backing root accepted")
	}
}

func TestStoragePrelockRejectsUnsafeMetadataWithoutCreatingLock(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(base, "state")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	configBase, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(configBase, "config.json")
	if err := os.WriteFile(config, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	expected := StorageExpectation{ConfigPath: config, StateRoot: root, MountPoint: filepath.Dir(root),
		VolumeUUID: "00112233-4455-6677-8899-aabbccddeeff"}
	if err := CheckStorage(expected); err == nil || !strings.Contains(err.Error(), "private directory") {
		t.Fatalf("unsafe backing root was not refused before lock: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(root, "locks")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unsafe backing root gained a lock directory: %v", err)
	}
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(config, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := CheckStorage(expected); err == nil || !strings.Contains(err.Error(), "private regular") {
		t.Fatalf("unsafe authority config was not refused before lock: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(root, "locks")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unsafe authority config gained a lock directory: %v", err)
	}
}

func TestStoragePrelockRejectsSymlinkedConfigPath(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(base, "state")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(base, "config.json")
	if err := os.WriteFile(config, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(base, "config-alias.json")
	if err := os.Symlink(config, alias); err != nil {
		t.Fatal(err)
	}
	expected := StorageExpectation{ConfigPath: alias, StateRoot: root, MountPoint: base,
		VolumeUUID: "00112233-4455-6677-8899-aabbccddeeff"}
	if err := CheckStorage(expected); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("symlinked authority path accepted: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(root, "locks")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("symlink refusal created lock: %v", err)
	}
}

func TestEnrollmentRejectsSameFilesystemBeforeCreatingConfig(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(base, "state")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(base, "enrolled.json")
	expected := StorageExpectation{ConfigPath: target, StateRoot: root, MountPoint: base,
		VolumeUUID: "00112233-4455-6677-8899-aabbccddeeff"}
	if err := WriteEnrolledConfig(expected, []byte("{}\n")); err == nil {
		t.Fatal("same-filesystem enrollment accepted")
	}
	if _, err := os.Lstat(target); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("refused enrollment wrote config: %v", err)
	}
}

func TestEnrollmentObservationRequiresSeparateFSIDAndExpectedUUID(t *testing.T) {
	expected := StorageExpectation{MountPoint: "/Volumes/Qualified", VolumeUUID: "00112233-4455-6677-8899-aabbccddeeff"}
	backing := syscall.Fsid{Val: [2]int32{1, 2}}
	output := syscall.Fsid{Val: [2]int32{3, 4}}
	if err := validateEnrollmentObservations(expected, output, backing, "/Volumes/Qualified", expected.VolumeUUID); err != nil {
		t.Fatal(err)
	}
	if err := validateEnrollmentObservations(expected, backing, backing, "/Volumes/Qualified", expected.VolumeUUID); err == nil {
		t.Fatal("same-filesystem output accepted")
	}
	if err := validateEnrollmentObservations(expected, output, backing, "/Volumes/Qualified", "10213243-5465-4768-899a-bbccddeeff00"); err == nil {
		t.Fatal("wrong APFS UUID accepted")
	}
}
