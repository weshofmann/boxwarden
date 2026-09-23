package main

import (
	"bytes"
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
		{"alpha_fixture=ext4 alpha_fixture=ext4", "", true},
		{"alpha_fixture=other", "", true},
	} {
		got, err := parseFixtureMode(tc.command)
		if (err != nil) != tc.bad || got != tc.want {
			t.Fatalf("parseFixtureMode(%q) = %q, %v", tc.command, got, err)
		}
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
