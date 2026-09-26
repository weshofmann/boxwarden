package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestFixtureModeIsExplicit(t *testing.T) {
	for _, tc := range []struct {
		command string
		want    string
		bad     bool
	}{
		{"console=hvc0", "zero", false},
		{"console=hvc0 alpha_fixture=ext4", "ext4", false},
		{"console=hvc0 alpha_fixture=copy", "copy", false},
		{"console=hvc0 alpha_fixture=export", "export", false},
		{"alpha_fixture=ext4 alpha_fixture=ext4", "", true},
		{"alpha_fixture=other", "", true},
	} {
		got, err := parseFixtureMode(tc.command)
		if (err != nil) != tc.bad || got != tc.want {
			t.Fatalf("parseFixtureMode(%q) = %q, %v", tc.command, got, err)
		}
	}
}

func TestCopySuperblockReportsExactUUIDWithoutChangingFixturePin(t *testing.T) {
	superblock := make([]byte, 1024)
	copy(superblock[0x38:], []byte{0x53, 0xef})
	copy(superblock[0x68:], []byte{0xe9, 0x15, 0x85, 0x5e, 0x80, 0x1c, 0x40, 0x5b, 0x9f, 0xb8, 0x7c, 0x8b, 0x62, 0xbd, 0x8f, 0x45})
	superblock[0x5c] = 0x04
	superblock[0x60] = 0x40
	superblock[0x3a] = 0x01
	uuid, err := inspectExt4Superblock(superblock)
	if err != nil || uuid != "e915855e-801c-405b-9fb8-7c8b62bd8f45" {
		t.Fatalf("inspect copy superblock = %q, %v", uuid, err)
	}
	if err := validateExt4FixtureSuperblock(superblock); err == nil {
		t.Fatal("fixed fixture unexpectedly accepted the copy UUID")
	}
}

func TestExt4IdentityRequiresMagicUUIDJournalExtentsAndCleanState(t *testing.T) {
	uuid, err := hex.DecodeString("2f1c6b8898494c5d9d20f3bc30bd77a1")
	if err != nil {
		t.Fatal(err)
	}
	superblock := make([]byte, 1024)
	copy(superblock[0x38:], []byte{0x53, 0xef})
	copy(superblock[0x68:], uuid)
	superblock[0x5c] = 0x04 // has_journal
	superblock[0x60] = 0x40 // extents
	superblock[0x3a] = 0x01 // clean
	if err := validateExt4FixtureSuperblock(superblock); err != nil {
		t.Fatal(err)
	}
	for _, offset := range []int{0x38, 0x68, 0x5c, 0x60, 0x3a} {
		broken := bytes.Clone(superblock)
		broken[offset] = 0
		if err := validateExt4FixtureSuperblock(broken); err == nil {
			t.Fatalf("accepted broken superblock offset %#x", offset)
		}
	}
	for name, mutate := range map[string]func([]byte){
		"error state":      func(sb []byte) { sb[0x3a] = 0x03 },
		"orphan state":     func(sb []byte) { sb[0x3a] = 0x05 },
		"journal recovery": func(sb []byte) { sb[0x60] |= 0x04 },
		"orphan recovery":  func(sb []byte) { sb[0x66] |= 0x01 },
	} {
		broken := bytes.Clone(superblock)
		mutate(broken)
		if err := validateExt4FixtureSuperblock(broken); err == nil {
			t.Fatalf("accepted ext4 superblock requiring %s", name)
		}
	}
	if err := validateExt4FixtureSuperblock(superblock[:100]); err == nil {
		t.Fatal("accepted truncated superblock")
	}
}

func TestAllowedFixtureFileRejectsChangedBytesAndSymlink(t *testing.T) {
	directory := t.TempDir()
	file := filepath.Join(directory, "proof.txt")
	if err := os.WriteFile(file, []byte(fixtureContent), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readAllowedFixtureFile(file); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("changed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readAllowedFixtureFile(file); err == nil {
		t.Fatal("accepted changed content")
	}
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/etc/passwd", file); err != nil {
		t.Fatal(err)
	}
	if _, err := readAllowedFixtureFile(file); err == nil {
		t.Fatal("accepted symlink")
	}
}

func TestCopyFileReadIsBoundedAndRefusesSymlink(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "boxwarden-alpha-synthetic.txt")
	data := []byte("synthetic persistence proof\n")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	want := sha256.Sum256(data)
	digest, size, err := readAllowedCopyFile(path)
	if err != nil || digest != hex.EncodeToString(want[:]) || size != int64(len(data)) {
		t.Fatalf("copy file = %s, %d, %v", digest, size, err)
	}
	if err := os.WriteFile(path, bytes.Repeat([]byte("x"), 4097), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := readAllowedCopyFile(path); err == nil {
		t.Fatal("accepted oversized copy proof")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/etc/passwd", path); err != nil {
		t.Fatal(err)
	}
	if _, _, err := readAllowedCopyFile(path); err == nil {
		t.Fatal("accepted copy proof symlink")
	}
}
