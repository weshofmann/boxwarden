// Package importx captures bounded, explicit host project input before a
// separate pinned guest transfer. It never exposes a live host directory.
package importx

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"syscall"

	"github.com/weshofmann/boxwarden/internal/diskreserve"
	"github.com/weshofmann/boxwarden/internal/privateacl"
)

const (
	maxFiles       = 256
	maxDirectories = 256
	maxDepth       = 8
	maxPathBytes   = 255
	maxFileBytes   = 4 << 20
	maxTotalBytes  = 16 << 20
	manifestName   = ".boxwarden-import-manifest.json"
)

type Entry struct {
	Path   string `json:"path"`
	Kind   string `json:"kind"`
	Size   int64  `json:"size,omitempty"`
	SHA256 string `json:"sha256,omitempty"`
}

type Snapshot struct {
	TransactionID  string  `json:"transaction_id"`
	Directory      string  `json:"directory"`
	Digest         string  `json:"digest"`
	FileCount      int     `json:"file_count"`
	DirectoryCount int     `json:"directory_count"`
	TotalBytes     int64   `json:"total_bytes"`
	Entries        []Entry `json:"entries"`
}

// CaptureSource copies one private, owner-controlled regular-file tree to a
// new private immutable-by-convention snapshot. Symlinks, hardlinks, special
// files, mutable shared directories, and ambiguous names fail closed. A
// caller must durably bind the returned digest and path before transfer.
func CaptureSource(ctx context.Context, sourcePath, stagingParent, transactionID string) (snapshot Snapshot, err error) {
	if !validUUID(transactionID) || !canonicalAbsolute(sourcePath) || !canonicalAbsolute(stagingParent) || sourcePath == stagingParent {
		return Snapshot{}, fmt.Errorf("invalid import source, staging parent, or transaction identity")
	}
	resolvedSource, err := filepath.EvalSymlinks(sourcePath)
	if err != nil {
		return Snapshot{}, err
	}
	resolvedParent, err := filepath.EvalSymlinks(stagingParent)
	if err != nil {
		return Snapshot{}, err
	}
	if pathContains(resolvedSource, resolvedParent) || pathContains(resolvedParent, resolvedSource) {
		return Snapshot{}, fmt.Errorf("import source and staging parent overlap")
	}
	source, err := openPrivateDirectory(sourcePath)
	if err != nil {
		return Snapshot{}, fmt.Errorf("import source: %w", err)
	}
	defer source.Close()
	parent, err := openPrivateDirectory(stagingParent)
	if err != nil {
		return Snapshot{}, fmt.Errorf("import staging parent: %w", err)
	}
	defer parent.Close()
	if _, err := parent.Lstat(transactionID); err == nil {
		return Snapshot{}, fmt.Errorf("import snapshot already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return Snapshot{}, err
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return Snapshot{}, err
	}
	temporary := ".import-" + transactionID + "-" + hex.EncodeToString(nonce[:])
	published := false
	err = diskreserve.Run(ctx, []string{stagingParent}, func(guarded context.Context) (captureErr error) {
		if err := parent.Mkdir(temporary, 0o700); err != nil {
			return err
		}
		createdInfo, err := parent.Lstat(temporary)
		if err != nil {
			return err
		}
		defer func() {
			if !published {
				captureErr = errors.Join(captureErr, removePrivateTree(parent, temporary, createdInfo))
			}
		}()
		stage, err := parent.OpenRoot(temporary)
		if err != nil {
			return err
		}
		defer stage.Close()
		stageInfo, err := stage.Stat(".")
		if err != nil || !os.SameFile(stageInfo, createdInfo) {
			return fmt.Errorf("import temporary directory changed while opening: %v", err)
		}
		snapshot = Snapshot{TransactionID: transactionID, Directory: filepath.Join(stagingParent, transactionID)}
		rootInfo, err := source.Stat(".")
		if err != nil {
			return err
		}
		if err := captureDirectory(guarded, source, stage, sourcePath, "", rootInfo, 0, &snapshot); err != nil {
			return err
		}
		if snapshot.FileCount == 0 {
			return fmt.Errorf("import source has no regular files")
		}
		raw, err := json.Marshal(struct {
			Version int     `json:"version"`
			Entries []Entry `json:"entries"`
		}{Version: 1, Entries: snapshot.Entries})
		if err != nil {
			return err
		}
		digest := sha256.Sum256(raw)
		snapshot.Digest = hex.EncodeToString(digest[:])
		manifest, err := stage.OpenFile(manifestName, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0o600)
		if err != nil {
			return err
		}
		if _, err := manifest.Write(append(raw, '\n')); err != nil {
			manifest.Close()
			return err
		}
		if err := manifest.Sync(); err != nil {
			manifest.Close()
			return err
		}
		if err := manifest.Close(); err != nil {
			return err
		}
		if err := syncRoot(stage); err != nil {
			return err
		}
		if err := guarded.Err(); err != nil {
			return err
		}
		stillNamed, err := parent.Lstat(temporary)
		if err != nil || !os.SameFile(stillNamed, createdInfo) {
			return fmt.Errorf("import temporary directory changed before publication: %v", err)
		}
		if _, err := parent.Lstat(transactionID); !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("import snapshot destination changed: %v", err)
		}
		if err := renameExclusive(parent, temporary, transactionID); err != nil {
			return err
		}
		published = true
		return syncRoot(parent)
	})
	if err != nil {
		if published {
			return snapshot, fmt.Errorf("import snapshot published but parent sync uncertain: %w", err)
		}
		return Snapshot{}, err
	}
	return snapshot, nil
}

func pathContains(parent, child string) bool {
	relative, err := filepath.Rel(parent, child)
	return err == nil && (relative == "." || relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)))
}

// removePrivateTree removes only the temporary tree through the already-open
// staging parent. A partial capture may contain nested directories.
func removePrivateTree(parent *os.Root, name string, expected os.FileInfo) error {
	named, err := parent.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil || !named.IsDir() || !os.SameFile(named, expected) {
		return fmt.Errorf("import temporary directory changed before cleanup: %v", err)
	}
	child, err := parent.OpenRoot(name)
	if err != nil {
		return err
	}
	opened, err := child.Stat(".")
	if err != nil || !os.SameFile(opened, expected) {
		child.Close()
		return fmt.Errorf("import temporary directory changed while opening for cleanup: %v", err)
	}
	directory, err := child.Open(".")
	if err != nil {
		child.Close()
		return err
	}
	entries, err := directory.ReadDir(-1)
	err = errors.Join(err, directory.Close())
	if err != nil {
		child.Close()
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			info, err := child.Lstat(entry.Name())
			if err != nil {
				child.Close()
				return err
			}
			if err := removePrivateTree(child, entry.Name(), info); err != nil {
				child.Close()
				return err
			}
		} else if err := child.Remove(entry.Name()); err != nil {
			child.Close()
			return err
		}
	}
	if err := child.Close(); err != nil {
		return err
	}
	return parent.Remove(name)
}

func captureDirectory(ctx context.Context, source, stage *os.Root, sourcePath, relative string, expected os.FileInfo, depth int, snapshot *Snapshot) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if depth > maxDepth {
		return fmt.Errorf("import source depth exceeds %d", maxDepth)
	}
	name := relative
	if name == "" {
		name = "."
	}
	directory, err := source.OpenFile(name, os.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	opened, err := directory.Stat()
	if err != nil || !os.SameFile(expected, opened) || !sameSourceInfo(expected, opened) {
		directory.Close()
		return fmt.Errorf("import source directory changed while opening: %v", err)
	}
	entries, readErr := directory.ReadDir(maxFiles + maxDirectories + 1)
	closeErr := directory.Close()
	if readErr != nil && !errors.Is(readErr, io.EOF) {
		return readErr
	}
	if closeErr != nil {
		return closeErr
	}
	if len(entries) > maxFiles+maxDirectories {
		return fmt.Errorf("import source has too many entries")
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		component := entry.Name()
		if !validComponent(component) || component == manifestName {
			return fmt.Errorf("unsafe import source name %q", component)
		}
		child := component
		if relative != "" {
			child = path.Join(relative, component)
		}
		if len(child) > maxPathBytes {
			return fmt.Errorf("import source path exceeds %d bytes", maxPathBytes)
		}
		info, err := source.Lstat(child)
		if err != nil {
			return err
		}
		if err := admitSourceInfo(filepath.Join(sourcePath, filepath.FromSlash(child)), info); err != nil {
			return fmt.Errorf("import source %q: %w", child, err)
		}
		if info.IsDir() {
			if depth == maxDepth || snapshot.DirectoryCount >= maxDirectories {
				return fmt.Errorf("import source directory limit exceeded")
			}
			if err := stage.Mkdir(child, 0o700); err != nil {
				return err
			}
			snapshot.DirectoryCount++
			snapshot.Entries = append(snapshot.Entries, Entry{Path: child, Kind: "directory"})
			if err := captureDirectory(ctx, source, stage, sourcePath, child, info, depth+1, snapshot); err != nil {
				return err
			}
			if err := syncChildRoot(stage, child); err != nil {
				return err
			}
			continue
		}
		if !info.Mode().IsRegular() || snapshot.FileCount >= maxFiles || info.Size() < 0 || info.Size() > maxFileBytes || snapshot.TotalBytes > maxTotalBytes-info.Size() {
			return fmt.Errorf("import source file type or size limit exceeded")
		}
		digest, err := copySourceFile(source, stage, child, info)
		if err != nil {
			return err
		}
		snapshot.FileCount++
		snapshot.TotalBytes += info.Size()
		snapshot.Entries = append(snapshot.Entries, Entry{Path: child, Kind: "file", Size: info.Size(), SHA256: digest})
	}
	after, err := source.Lstat(name)
	if err != nil || !os.SameFile(expected, after) || !sameSourceInfo(expected, after) {
		return fmt.Errorf("import source directory changed during capture: %v", err)
	}
	return nil
}

func copySourceFile(source, stage *os.Root, name string, initial os.FileInfo) (string, error) {
	input, err := source.OpenFile(name, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return "", err
	}
	defer input.Close()
	opened, err := input.Stat()
	if err != nil || !os.SameFile(initial, opened) || !sameSourceInfo(initial, opened) {
		return "", fmt.Errorf("import source file changed while opening: %v", err)
	}
	output, err := stage.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return "", err
	}
	defer output.Close()
	hasher := sha256.New()
	if _, err := io.CopyN(io.MultiWriter(output, hasher), input, initial.Size()); err != nil {
		return "", err
	}
	var extra [1]byte
	if count, err := input.Read(extra[:]); count != 0 || err != io.EOF {
		return "", fmt.Errorf("import source file grew during capture: %v", err)
	}
	after, err := input.Stat()
	if err != nil || !os.SameFile(initial, after) || !sameSourceInfo(initial, after) {
		return "", fmt.Errorf("import source file changed during capture: %v", err)
	}
	pathInfo, err := source.Lstat(name)
	if err != nil || !os.SameFile(initial, pathInfo) || !sameSourceInfo(initial, pathInfo) {
		return "", fmt.Errorf("import source path changed during capture: %v", err)
	}
	if err := output.Sync(); err != nil {
		return "", err
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

func openPrivateDirectory(name string) (*os.Root, error) {
	if !canonicalAbsolute(name) {
		return nil, fmt.Errorf("private directory path must be canonical and absolute")
	}
	info, err := os.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode().Perm() != 0o700 {
		return nil, fmt.Errorf("private directory must be mode 0700")
	}
	if err := admitSourceInfo(name, info); err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(name)
	if err != nil {
		return nil, err
	}
	opened, err := root.Stat(".")
	if err != nil || !os.SameFile(info, opened) {
		root.Close()
		return nil, fmt.Errorf("private directory changed while opening: %v", err)
	}
	return root, nil
}

func admitSourceInfo(name string, info os.FileInfo) error {
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() && !info.Mode().IsRegular() || info.Mode().Perm()&0o022 != 0 {
		return fmt.Errorf("source is a link, special file, or group/other writable")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Geteuid()) || info.Mode().IsRegular() && stat.Nlink != 1 {
		return fmt.Errorf("source is not solely owned by current user")
	}
	return privateacl.Check(name, info, privateacl.OSInspector{})
}

func sameSourceInfo(a, b os.FileInfo) bool {
	return a.Size() == b.Size() && a.Mode() == b.Mode() && a.ModTime().Equal(b.ModTime())
}

func canonicalAbsolute(name string) bool {
	return filepath.IsAbs(name) && filepath.Clean(name) == name && name != "/" && len(name) <= 4096
}

func validUUID(value string) bool {
	if len(value) != 36 || value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' {
		return false
	}
	for i, char := range value {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			continue
		}
		if !strings.ContainsRune("0123456789abcdef", char) {
			return false
		}
	}
	return true
}

func validComponent(value string) bool {
	if len(value) == 0 || len(value) > 63 || value == "." || value == ".." || value == ".git" {
		return false
	}
	for i, char := range value {
		if !(char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || i > 0 && (char == '.' || char == '_' || char == '-')) {
			return false
		}
	}
	return true
}

func syncRoot(root *os.Root) error {
	directory, err := root.Open(".")
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

func syncChildRoot(root *os.Root, name string) error {
	child, err := root.OpenRoot(name)
	if err != nil {
		return err
	}
	defer child.Close()
	return syncRoot(child)
}
