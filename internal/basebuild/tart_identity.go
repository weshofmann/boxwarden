package basebuild

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"
	"syscall"

	"github.com/weshofmann/boxwarden/internal/backend"
)

// CandidateIdentity binds cache admission to the exact stopped Tart bundle.
// Reading a sparse disk hashes its logical bytes, which can cost many GiB of
// I/O on admission and every reuse. Cancellation stops that read promptly.
func (v *TartVM) CandidateIdentity(ctx context.Context, id string) (string, error) {
	if v == nil {
		return "", fmt.Errorf("Tart VM adapter is nil")
	}
	return fingerprintTartBundle(ctx, v.config.TartHome, id)
}

func fingerprintTartBundle(ctx context.Context, home, id string) (string, error) {
	if err := backend.ValidateObjectID(id); err != nil {
		return "", err
	}
	if !absoluteClean(home) {
		return "", fmt.Errorf("Tart home must be canonical and absolute")
	}
	resolved, err := filepath.EvalSymlinks(home)
	if err != nil || resolved != home {
		return "", fmt.Errorf("Tart home has a symlinked ancestor: %v", err)
	}
	for _, path := range []string{home, filepath.Join(home, "vms"), filepath.Join(home, "vms", id)} {
		info, err := os.Lstat(path)
		if err != nil {
			return "", err
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !info.IsDir() || !ok || int(stat.Uid) != os.Getuid() || info.Mode().Perm()&0022 != 0 {
			return "", fmt.Errorf("Tart bundle ancestry has unsafe type, owner, or write mode")
		}
	}
	bundle := filepath.Join(home, "vms", id)
	root, err := os.OpenRoot(bundle)
	if err != nil {
		return "", err
	}
	defer root.Close()
	pathInfo, err := os.Lstat(bundle)
	if err != nil {
		return "", err
	}
	opened, err := root.Stat(".")
	if err != nil || !os.SameFile(pathInfo, opened) {
		return "", fmt.Errorf("Tart bundle changed while opening")
	}
	entries, err := os.ReadDir(bundle)
	if err != nil {
		return "", err
	}
	if len(entries) != 3 {
		return "", fmt.Errorf("Tart bundle layout is unknown")
	}
	want := map[string]int64{"config.json": 4 << 20, "disk.img": 2 << 40, "nvram.bin": 64 << 20}
	for _, entry := range entries {
		if _, ok := want[entry.Name()]; !ok {
			return "", fmt.Errorf("Tart bundle layout is unknown")
		}
	}
	h := sha256.New()
	_, _ = h.Write([]byte("boxwarden-tart-candidate-identity-v1\x00" + id + "\x00"))
	writeFileIdentity(h, pathInfo)
	for _, name := range []string{"config.json", "disk.img", "nvram.bin"} {
		if err := hashBundleFile(ctx, root, name, want[name], h); err != nil {
			return "", fmt.Errorf("Tart %s identity: %w", name, err)
		}
	}
	finalPath, err := os.Lstat(bundle)
	if err != nil || !os.SameFile(pathInfo, finalPath) {
		return "", fmt.Errorf("Tart bundle path changed while hashing")
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

func writeFileIdentity(h hash.Hash, info os.FileInfo) {
	stat := info.Sys().(*syscall.Stat_t)
	var data [24]byte
	binary.BigEndian.PutUint64(data[0:8], uint64(stat.Dev))
	binary.BigEndian.PutUint64(data[8:16], stat.Ino)
	binary.BigEndian.PutUint64(data[16:24], uint64(info.Size()))
	_, _ = h.Write(data[:])
}

func hashBundleFile(ctx context.Context, root *os.Root, name string, maximum int64, h hash.Hash) error {
	entry, err := root.Lstat(name)
	if err != nil {
		return err
	}
	stat, ok := entry.Sys().(*syscall.Stat_t)
	if !entry.Mode().IsRegular() || !ok || int(stat.Uid) != os.Getuid() || stat.Nlink != 1 || entry.Mode().Perm()&0022 != 0 || entry.Size() <= 0 || entry.Size() > maximum {
		return fmt.Errorf("unsafe Tart bundle file type, owner, links, mode, or size")
	}
	file, err := root.OpenFile(name, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(entry, opened) {
		return fmt.Errorf("Tart bundle file changed while opening")
	}
	_, _ = h.Write([]byte(name + "\x00"))
	writeFileIdentity(h, entry)
	content := sha256.New()
	buf := make([]byte, 1<<20)
	var count int64
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, readErr := file.Read(buf)
		if n > 0 {
			count += int64(n)
			if count > maximum {
				return fmt.Errorf("file grew beyond limit")
			}
			_, _ = content.Write(buf[:n])
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return readErr
		}
	}
	if count != entry.Size() {
		return fmt.Errorf("file length changed while reading")
	}
	after, err := file.Stat()
	if err != nil || !os.SameFile(entry, after) || after.Size() != entry.Size() || after.ModTime() != entry.ModTime() {
		return fmt.Errorf("file changed while reading")
	}
	current, err := root.Lstat(name)
	if err != nil || !os.SameFile(entry, current) || current.Size() != entry.Size() || current.ModTime() != entry.ModTime() {
		return fmt.Errorf("file path changed while reading")
	}
	_, _ = h.Write(content.Sum(nil))
	return nil
}

var _ CandidateIdentity = (*TartVM)(nil)
