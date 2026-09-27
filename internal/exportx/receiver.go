// Package exportx receives an untrusted, typed export stream into a private
// host directory. It does not decide whether the guest was offline, authenticate
// the sender, or inspect the meaning of exported bytes.
//
// Version 1 starts with "BWEX", a big-endian uint16 version, and a 16-byte
// transaction ID. Each record has a 47-byte fixed header: kind (uint8), path
// length (uint16), chunk length (uint32), declared length (uint64), and a
// SHA-256 digest. Path and chunk bytes follow. Kinds are directory (1), file
// begin (2), chunk (3), file end (4), and terminal (5). Unused fields must be
// zero. File begin declares the exact file length, file end carries its hash,
// and terminal declares the exact total payload length. Directories precede
// descendants. The terminal record must be followed immediately by EOF.
package exportx

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"syscall"
)

const (
	headerSize    = 22
	recordSize    = 47
	maxPathBytes  = 512
	maxComponents = 16
	maxComponent  = 64
	hardMaxChunk  = 1 << 20

	recordDirectory = 1
	recordFile      = 2
	recordChunk     = 3
	recordFileEnd   = 4
	recordTerminal  = 5
)

// Options is a host-selected resource policy. Parent must be a private 0700
// directory owned by the receiver process. A nonzero MinFreeBytes is checked
// against the filesystem containing Parent before and during transfer. This
// is a sampled free-space floor, not an allocation reservation; callers must
// account for concurrent host writes and their own global disk policy.
type Options struct {
	Parent        string
	TransactionID [16]byte
	// Selected, when provided, binds every received path and requires every
	// selected path to appear before publication. Production callers must set it.
	Selected       []string
	MaxChunkBytes  uint32
	MaxFileBytes   uint64
	MaxTotalBytes  uint64
	MaxFiles       uint32
	MaxDirectories uint32
	MinFreeBytes   uint64
}

type record struct {
	kind     byte
	pathLen  uint16
	chunkLen uint32
	declared uint64
	digest   [32]byte
}

type activeFile struct {
	file     *os.File
	hash     hash.Hash
	declared uint64
	written  uint64
}

type pathEntry struct {
	kind byte
	name string
}

// ReceiveSelectedExport is the production publication entry point. It fails
// closed if the caller omits the journal-bound host selection.
func ReceiveSelectedExport(ctx context.Context, stream io.ReadCloser, options Options) (string, error) {
	if len(options.Selected) == 0 {
		if stream != nil {
			_ = stream.Close()
		}
		return "", fmt.Errorf("selected export requires host path selection")
	}
	return Receive(ctx, stream, options)
}

// Receive owns and closes stream. It requires EOF immediately after the
// terminal record. On failure before publication, it removes private staging.
// On success it returns Parent/<lowercase transaction hex>. The caller must
// supply a context/deadline appropriate to its transport; cancellation closes
// stream to interrupt ordinary blocking reads. If the rename succeeds but a
// subsequent parent sync or path revalidation fails, Receive returns both the
// published path and an error so recovery can inspect that exact transaction.
func Receive(ctx context.Context, stream io.ReadCloser, options Options) (string, error) {
	if stream == nil {
		return "", fmt.Errorf("nil stream")
	}
	defer stream.Close()
	if err := validateOptions(options); err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	stopClose := context.AfterFunc(ctx, func() { _ = stream.Close() })
	defer stopClose()

	parentFile, parentRoot, initialInfo, err := openPrivateParent(options.Parent)
	if err != nil {
		return "", err
	}
	defer parentFile.Close()
	defer parentRoot.Close()
	if err := checkSpace(parentFile, options.MinFreeBytes, 0); err != nil {
		return "", err
	}

	var header [headerSize]byte
	if _, err := io.ReadFull(stream, header[:]); err != nil {
		return "", streamError(ctx, fmt.Errorf("stream header: %w", err))
	}
	if string(header[:4]) != "BWEX" || binary.BigEndian.Uint16(header[4:6]) != 1 {
		return "", fmt.Errorf("unsupported export stream")
	}
	if string(header[6:]) != string(options.TransactionID[:]) {
		return "", fmt.Errorf("transaction mismatch")
	}

	stagingName, err := newStaging(parentRoot)
	if err != nil {
		return "", err
	}
	published := false
	defer func() {
		if !published {
			_ = parentRoot.RemoveAll(stagingName)
		}
	}()
	stage, err := parentRoot.OpenRoot(stagingName)
	if err != nil {
		return "", err
	}
	defer stage.Close()

	seen := make(map[string]pathEntry)
	directories := []string{"."}
	var current *activeFile
	defer func() {
		if current != nil {
			_ = current.file.Close()
		}
	}()
	var files, dirs uint32
	var total uint64
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		rec, err := readRecord(stream)
		if err != nil {
			return "", streamError(ctx, err)
		}
		if rec.pathLen > maxPathBytes {
			return "", fmt.Errorf("path length exceeds limit")
		}
		if rec.kind != recordChunk && rec.chunkLen != 0 {
			return "", fmt.Errorf("unexpected record payload")
		}
		if rec.kind == recordChunk && rec.chunkLen > options.MaxChunkBytes {
			return "", fmt.Errorf("chunk length exceeds limit")
		}
		var name string
		if rec.pathLen != 0 {
			pathBytes := make([]byte, rec.pathLen)
			if _, err := io.ReadFull(stream, pathBytes); err != nil {
				return "", streamError(ctx, fmt.Errorf("record path: %w", err))
			}
			name = string(pathBytes)
		}
		switch rec.kind {
		case recordDirectory:
			if current != nil || rec.pathLen == 0 || rec.declared != 0 || !zeroDigest(rec.digest) {
				return "", fmt.Errorf("invalid directory record")
			}
			if err := admitPath(name, seen); err != nil {
				return "", err
			}
			if err := admitSelectedPath(name, recordDirectory, options.Selected); err != nil {
				return "", err
			}
			if dirs >= options.MaxDirectories {
				return "", fmt.Errorf("directory count exceeds limit")
			}
			if err := stage.Mkdir(name, 0o700); err != nil {
				return "", err
			}
			seen[strings.ToLower(name)] = pathEntry{kind: recordDirectory, name: name}
			directories = append(directories, name)
			dirs++
		case recordFile:
			if current != nil || rec.pathLen == 0 || !zeroDigest(rec.digest) {
				return "", fmt.Errorf("invalid file record")
			}
			if err := admitPath(name, seen); err != nil {
				return "", err
			}
			if err := admitSelectedPath(name, recordFile, options.Selected); err != nil {
				return "", err
			}
			if files >= options.MaxFiles || rec.declared > options.MaxFileBytes || rec.declared > options.MaxTotalBytes-total {
				return "", fmt.Errorf("file size or count exceeds limit")
			}
			file, err := stage.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0o600)
			if err != nil {
				return "", err
			}
			current = &activeFile{file: file, hash: sha256.New(), declared: rec.declared}
			seen[strings.ToLower(name)] = pathEntry{kind: recordFile, name: name}
			files++
		case recordChunk:
			if current == nil || rec.pathLen != 0 || rec.chunkLen == 0 || rec.declared != 0 || !zeroDigest(rec.digest) {
				return "", fmt.Errorf("invalid chunk record")
			}
			if uint64(rec.chunkLen) > current.declared-current.written || uint64(rec.chunkLen) > options.MaxTotalBytes-total {
				return "", fmt.Errorf("chunk exceeds declared size or total limit")
			}
			if err := copyChunk(ctx, stream, current, rec.chunkLen, parentFile, options.MinFreeBytes); err != nil {
				return "", streamError(ctx, err)
			}
			total += uint64(rec.chunkLen)
		case recordFileEnd:
			if current == nil || rec.pathLen != 0 || rec.declared != 0 || current.written != current.declared {
				return "", fmt.Errorf("invalid file-end record")
			}
			if !equalDigest(current.hash.Sum(nil), rec.digest) {
				return "", fmt.Errorf("file digest mismatch")
			}
			if err := current.file.Sync(); err != nil {
				return "", err
			}
			if err := current.file.Close(); err != nil {
				return "", err
			}
			current = nil
		case recordTerminal:
			if current != nil || rec.pathLen != 0 || rec.declared != total || !zeroDigest(rec.digest) {
				return "", fmt.Errorf("invalid terminal record")
			}
			for _, selected := range options.Selected {
				entry, ok := seen[strings.ToLower(selected)]
				if !ok || entry.name != selected {
					return "", fmt.Errorf("selected export path is absent")
				}
			}
			var tail [1]byte
			n, err := io.ReadFull(stream, tail[:])
			if n != 0 || !errors.Is(err, io.EOF) {
				if err == nil {
					return "", fmt.Errorf("trailing stream bytes")
				}
				return "", streamError(ctx, fmt.Errorf("trailing stream read: %w", err))
			}
			if err := syncDirectories(stage, directories); err != nil {
				return "", err
			}
			if err := checkSpace(parentFile, options.MinFreeBytes, 0); err != nil {
				return "", err
			}
			if err := samePrivateParent(options.Parent, initialInfo, parentFile); err != nil {
				return "", err
			}
			if err := ctx.Err(); err != nil {
				return "", err
			}
			finalName := hex.EncodeToString(options.TransactionID[:])
			if err := renameNoReplace(int(parentFile.Fd()), stagingName, finalName); err != nil {
				return "", fmt.Errorf("publish export: %w", err)
			}
			published = true
			finalPath := filepath.Join(options.Parent, finalName)
			if err := syncDirectory(parentFile); err != nil {
				return finalPath, fmt.Errorf("export published but parent sync failed: %w", err)
			}
			if err := samePrivateParent(options.Parent, initialInfo, parentFile); err != nil {
				return finalPath, fmt.Errorf("export published but parent path changed: %w", err)
			}
			return finalPath, nil
		default:
			return "", fmt.Errorf("unknown record type %d", rec.kind)
		}
	}
}

func validateOptions(o Options) error {
	if o.Parent == "" || o.TransactionID == [16]byte{} || o.MaxChunkBytes == 0 || o.MaxChunkBytes > hardMaxChunk || o.MaxFileBytes == 0 || o.MaxTotalBytes == 0 || o.MaxFiles == 0 || o.MaxDirectories == 0 || o.MinFreeBytes == 0 {
		return fmt.Errorf("invalid export receiver options")
	}
	if len(o.Selected) > 1024 {
		return fmt.Errorf("too many selected export paths")
	}
	selected := make(map[string]bool, len(o.Selected))
	for _, name := range o.Selected {
		folded := strings.ToLower(name)
		if !validPath(name) || selected[folded] {
			return fmt.Errorf("invalid or colliding selected export path")
		}
		selected[folded] = true
	}
	return nil
}

func admitSelectedPath(name string, kind byte, selected []string) error {
	if len(selected) == 0 {
		return nil
	}
	for _, choice := range selected {
		if name == choice || strings.HasPrefix(name, choice+"/") ||
			(kind == recordDirectory && strings.HasPrefix(choice, name+"/")) {
			return nil
		}
	}
	return fmt.Errorf("received path is outside host selection")
}

func readRecord(r io.Reader) (record, error) {
	var raw [recordSize]byte
	if _, err := io.ReadFull(r, raw[:]); err != nil {
		return record{}, fmt.Errorf("record header: %w", err)
	}
	var result record
	result.kind = raw[0]
	result.pathLen = binary.BigEndian.Uint16(raw[1:3])
	result.chunkLen = binary.BigEndian.Uint32(raw[3:7])
	result.declared = binary.BigEndian.Uint64(raw[7:15])
	copy(result.digest[:], raw[15:47])
	return result, nil
}

func validPath(name string) bool {
	if len(name) == 0 || len(name) > maxPathBytes || strings.HasPrefix(name, "/") || strings.HasSuffix(name, "/") {
		return false
	}
	components := strings.Split(name, "/")
	if len(components) > maxComponents {
		return false
	}
	for _, component := range components {
		if component == "" || component == "." || component == ".." || len(component) > maxComponent {
			return false
		}
		for i := 0; i < len(component); i++ {
			c := component[i]
			if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '.' || c == '_' || c == '-') {
				return false
			}
		}
	}
	return true
}

func admitPath(name string, seen map[string]pathEntry) error {
	if !validPath(name) {
		return fmt.Errorf("unsafe export path")
	}
	folded := strings.ToLower(name)
	if _, exists := seen[folded]; exists {
		return fmt.Errorf("duplicate or case-folded export path")
	}
	parent := path.Dir(name)
	if parent != "." {
		entry, exists := seen[strings.ToLower(parent)]
		if !exists || entry.kind != recordDirectory || entry.name != parent {
			return fmt.Errorf("export parent is absent, not a directory, or has different case")
		}
	}
	return nil
}

func zeroDigest(d [32]byte) bool { return d == [32]byte{} }

func equalDigest(actual []byte, expected [32]byte) bool { return bytes.Equal(actual, expected[:]) }

func copyChunk(ctx context.Context, stream io.Reader, current *activeFile, size uint32, parent *os.File, minFree uint64) error {
	var buffer [32 * 1024]byte
	remaining := uint64(size)
	for remaining > 0 {
		if err := ctx.Err(); err != nil {
			return err
		}
		count := uint64(len(buffer))
		if remaining < count {
			count = remaining
		}
		if err := checkSpace(parent, minFree, count); err != nil {
			return err
		}
		if _, err := io.ReadFull(stream, buffer[:count]); err != nil {
			return fmt.Errorf("chunk data: %w", err)
		}
		if _, err := current.file.Write(buffer[:count]); err != nil {
			return err
		}
		if _, err := current.hash.Write(buffer[:count]); err != nil {
			return err
		}
		current.written += count
		remaining -= count
	}
	return nil
}

func streamError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}

func newStaging(parent *os.Root) (string, error) {
	for attempt := 0; attempt < 4; attempt++ {
		var entropy [16]byte
		if _, err := rand.Read(entropy[:]); err != nil {
			return "", err
		}
		name := ".incoming-" + hex.EncodeToString(entropy[:])
		if err := parent.Mkdir(name, 0o700); err == nil {
			return name, nil
		} else if !errors.Is(err, os.ErrExist) {
			return "", err
		}
	}
	return "", fmt.Errorf("unable to allocate staging directory")
}

func syncDirectories(root *os.Root, paths []string) error {
	for i := len(paths) - 1; i >= 0; i-- {
		dir, err := root.Open(paths[i])
		if err != nil {
			return err
		}
		err = dir.Sync()
		closeErr := dir.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}

func syncDirectory(file *os.File) error { return file.Sync() }
