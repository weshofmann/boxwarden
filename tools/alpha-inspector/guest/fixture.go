package main

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strings"
	"syscall"
)

const fixtureUUID = "2f1c6b88-9849-4c5d-9d20-f3bc30bd77a1"
const fixtureContent = "boxwarden synthetic ext4 proof v1\n"

func parseFixtureMode(commandLine string) (string, error) {
	mode := "zero"
	seen := false
	for _, field := range strings.Fields(commandLine) {
		if !strings.HasPrefix(field, "alpha_fixture=") {
			continue
		}
		if seen {
			return "", fmt.Errorf("duplicate fixture selector")
		}
		seen = true
		mode = strings.TrimPrefix(field, "alpha_fixture=")
		if mode != "ext4" {
			return "", fmt.Errorf("unknown fixture selector")
		}
	}
	return mode, nil
}

// validateExt4FixtureSuperblock checks the whole-device superblock before
// mount. UUID is an identity check, not an integrity check; the later kernel
// mount and exact allowlisted content read remain necessary.
func validateExt4FixtureSuperblock(sb []byte) error {
	if len(sb) != 1024 || !bytes.Equal(sb[0x38:0x3a], []byte{0x53, 0xef}) {
		return fmt.Errorf("missing ext4 superblock magic")
	}
	uuid := hex.EncodeToString(sb[0x68:0x78])
	want := strings.ReplaceAll(fixtureUUID, "-", "")
	if uuid != want {
		return fmt.Errorf("synthetic filesystem UUID mismatch")
	}
	if binary.LittleEndian.Uint16(sb[0x3a:0x3c])&1 == 0 {
		return fmt.Errorf("synthetic filesystem is not clean")
	}
	if binary.LittleEndian.Uint32(sb[0x5c:0x60])&0x04 == 0 {
		return fmt.Errorf("synthetic filesystem lacks journal")
	}
	if binary.LittleEndian.Uint32(sb[0x60:0x64])&0x40 == 0 {
		return fmt.Errorf("synthetic filesystem lacks extents")
	}
	return nil
}

func readAllowedFixtureFile(path string) ([]byte, error) {
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("open fixture proof: %w", err)
	}
	file := os.NewFile(uintptr(fd), path)
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() != int64(len(fixtureContent)) {
		return nil, fmt.Errorf("fixture proof is not exact regular file")
	}
	data, err := io.ReadAll(io.LimitReader(file, int64(len(fixtureContent))+1))
	if err != nil || string(data) != fixtureContent {
		return nil, fmt.Errorf("fixture proof content mismatch")
	}
	return data, nil
}
