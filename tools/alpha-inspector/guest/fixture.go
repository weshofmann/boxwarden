package main

import (
	"bytes"
	"crypto/sha256"
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
		if mode != "ext4" && mode != "copy" && mode != "export" {
			return "", fmt.Errorf("unknown fixture selector")
		}
	}
	return mode, nil
}

// validateExt4FixtureSuperblock checks the whole-device superblock before
// mount. UUID is an identity check, not an integrity check; the later kernel
// mount and exact allowlisted content read remain necessary.
func inspectExt4Superblock(sb []byte) (string, error) {
	if len(sb) != 1024 || !bytes.Equal(sb[0x38:0x3a], []byte{0x53, 0xef}) {
		return "", fmt.Errorf("missing ext4 superblock magic")
	}
	uuid := hex.EncodeToString(sb[0x68:0x78])
	if uuid == strings.Repeat("0", 32) {
		return "", fmt.Errorf("missing ext4 filesystem UUID")
	}
	if binary.LittleEndian.Uint16(sb[0x3a:0x3c]) != 1 {
		return "", fmt.Errorf("ext4 filesystem is not clean")
	}
	if binary.LittleEndian.Uint32(sb[0x5c:0x60])&0x04 == 0 {
		return "", fmt.Errorf("ext4 filesystem lacks journal")
	}
	incompatible := binary.LittleEndian.Uint32(sb[0x60:0x64])
	if incompatible&0x40 == 0 {
		return "", fmt.Errorf("ext4 filesystem lacks extents")
	}
	if incompatible&0x04 != 0 || binary.LittleEndian.Uint32(sb[0x64:0x68])&0x10000 != 0 {
		return "", fmt.Errorf("ext4 filesystem requires recovery")
	}
	return uuid[:8] + "-" + uuid[8:12] + "-" + uuid[12:16] + "-" + uuid[16:20] + "-" + uuid[20:], nil
}

func validateExt4FixtureSuperblock(sb []byte) error {
	uuid, err := inspectExt4Superblock(sb)
	if err != nil {
		return err
	}
	if uuid != fixtureUUID {
		return fmt.Errorf("synthetic filesystem UUID mismatch")
	}
	return nil
}

func readAllowedCopyFile(path string) (string, int64, error) {
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return "", 0, fmt.Errorf("open synthetic copy proof: %w", err)
	}
	file := os.NewFile(uintptr(fd), path)
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > 4096 {
		return "", 0, fmt.Errorf("synthetic copy proof is not a bounded regular file")
	}
	data, err := io.ReadAll(io.LimitReader(file, 4097))
	if err != nil || int64(len(data)) != info.Size() {
		return "", 0, fmt.Errorf("synthetic copy proof read changed or failed")
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]), info.Size(), nil
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
