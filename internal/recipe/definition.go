package recipe

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// guestDefinitionFiles is the fixed source set consumed by seed rendering,
// ISO remastering, and finalization. Adding a new build input requires adding
// it here so an old prepared-image key cannot silently cover new behavior.
var guestDefinitionFiles = []string{
	"artifacts.lock.json",
	"artifacts/boxwarden-guest-bootstrap",
	"autoinstall/meta-data",
	"autoinstall/user-data",
	"finalize-golden.sh",
	"remaster-golden-iso.sh",
	"render-golden-seed.sh",
}

// GuestDefinitionDigest identifies the tracked guest build inputs by exact
// filename, byte length, and content. It is a cache input, not an attestation
// of the resulting VM or the mutable package repositories used during build.
func GuestDefinitionDigest(root string) (string, error) {
	if !filepath.IsAbs(root) || filepath.Clean(root) != root {
		return "", fmt.Errorf("guest definition root must be canonical and absolute")
	}
	hash := sha256.New()
	_, _ = hash.Write([]byte("boxwarden-guest-definition-v1\x00"))
	var length [8]byte
	for _, name := range guestDefinitionFiles {
		filename := filepath.Join(root, name)
		entry, err := os.Lstat(filename)
		if err != nil {
			return "", fmt.Errorf("inspect guest definition %s: %w", name, err)
		}
		if !entry.Mode().IsRegular() {
			return "", fmt.Errorf("guest definition %s must be a regular file", name)
		}
		file, err := os.Open(filename)
		if err != nil {
			return "", fmt.Errorf("open guest definition %s: %w", name, err)
		}
		opened, err := file.Stat()
		if err != nil || !opened.Mode().IsRegular() || !os.SameFile(entry, opened) {
			file.Close()
			return "", fmt.Errorf("guest definition %s changed while opening", name)
		}
		_, _ = hash.Write([]byte(name))
		_, _ = hash.Write([]byte{0})
		binary.BigEndian.PutUint64(length[:], uint64(opened.Size()))
		_, _ = hash.Write(length[:])
		copied, copyErr := io.Copy(hash, file)
		closedErr := file.Close()
		if copyErr != nil || closedErr != nil || copied != opened.Size() {
			return "", fmt.Errorf("read guest definition %s: %v, %v", name, copyErr, closedErr)
		}
	}
	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}
