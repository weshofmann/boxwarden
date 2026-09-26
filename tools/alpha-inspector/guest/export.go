package main

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"syscall"
)

type guestExportLimits struct {
	maxFile        int64
	maxTotal       int64
	maxFiles       int
	maxDirectories int
}

var defaultGuestExportLimits = guestExportLimits{
	maxFile: 256 << 20, maxTotal: 256 << 20,
	maxFiles: 4096, maxDirectories: 4096,
}

type guestExportWriter struct {
	ctx    context.Context
	root   *os.Root
	output io.Writer
	limits guestExportLimits
	seen   map[string]string
	walked map[string]bool
	total  int64
	files  int
	dirs   int
}

// writeSelectedExport serializes only selected regular files and directories
// from an inspector-mounted filesystem. The caller must first prove the block
// device and ext4 mount are read-only and bind the request to the host journal.
// An error may leave an incomplete stream; the host receiver never publishes
// such a stream because it requires the terminal record and immediate EOF.
func writeSelectedExport(ctx context.Context, output io.Writer, rootPath string, transaction [16]byte, selected []string, limits guestExportLimits) error {
	total, err := writeSelectedExportBody(ctx, output, rootPath, transaction, selected, limits)
	if err != nil {
		return err
	}
	return writeExportTerminal(output, total)
}

// writeSelectedExportBody deliberately omits the terminal record. A booting
// inspector writes it only after the read-only ext4 mount has unmounted, so a
// failed unmount cannot leave a publishable complete stream.
func writeSelectedExportBody(ctx context.Context, output io.Writer, rootPath string, transaction [16]byte, selected []string, limits guestExportLimits) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if output == nil || transaction == [16]byte{} || len(selected) == 0 || len(selected) > 1024 ||
		limits.maxFile <= 0 || limits.maxFile > defaultGuestExportLimits.maxFile ||
		limits.maxTotal <= 0 || limits.maxTotal > defaultGuestExportLimits.maxTotal ||
		limits.maxFiles <= 0 || limits.maxFiles > defaultGuestExportLimits.maxFiles ||
		limits.maxDirectories <= 0 || limits.maxDirectories > defaultGuestExportLimits.maxDirectories {
		return 0, fmt.Errorf("invalid selected export request")
	}
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		return 0, err
	}
	defer root.Close()
	writer := guestExportWriter{ctx: ctx, root: root, output: output, limits: limits, seen: make(map[string]string), walked: make(map[string]bool)}
	var header [22]byte
	copy(header[:6], []byte{'B', 'W', 'E', 'X', 0, 1})
	copy(header[6:], transaction[:])
	if err := writeAll(output, header[:]); err != nil {
		return 0, err
	}
	for _, name := range selected {
		if err := writer.exportPath(name); err != nil {
			return 0, err
		}
	}
	return writer.total, nil
}

func writeExportTerminal(output io.Writer, total int64) error {
	if output == nil || total < 0 || total > defaultGuestExportLimits.maxTotal {
		return fmt.Errorf("invalid export terminal length")
	}
	return writeFrame(output, 5, "", nil, uint64(total), [32]byte{})
}

func (w *guestExportWriter) exportPath(name string) error {
	if !validGuestExportPath(name) {
		return fmt.Errorf("unsupported export path")
	}
	if err := w.ctx.Err(); err != nil {
		return err
	}
	ancestors := prefixes(name)
	for _, parent := range ancestors[:len(ancestors)-1] {
		if err := w.exportDirectory(parent, false); err != nil {
			return err
		}
	}
	info, err := w.safeInfo(name)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return w.exportDirectory(name, true)
	}
	if info.Mode().IsRegular() {
		return w.exportFile(name, info)
	}
	return fmt.Errorf("unsupported export object type")
}

func (w *guestExportWriter) exportDirectory(name string, descend bool) error {
	info, err := w.safeInfo(name)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("export parent is not a plain directory: %v", err)
	}
	if fresh, err := w.admitName(name); err != nil {
		return err
	} else if fresh {
		if w.dirs >= w.limits.maxDirectories {
			return fmt.Errorf("export directory count exceeded")
		}
		if err := writeFrame(w.output, 1, name, nil, 0, [32]byte{}); err != nil {
			return err
		}
		w.dirs++
	}
	if !descend {
		return nil
	}
	if w.walked[name] {
		return nil
	}
	w.walked[name] = true
	directory, err := w.root.Open(name)
	if err != nil {
		return err
	}
	opened, err := directory.Stat()
	if err != nil || !os.SameFile(info, opened) || !opened.IsDir() {
		directory.Close()
		return fmt.Errorf("export directory changed while opening: %v", err)
	}
	maxEntries := w.limits.maxFiles + w.limits.maxDirectories
	entries, readErr := directory.ReadDir(maxEntries + 1)
	closeErr := directory.Close()
	if err := errors.Join(nonEOF(readErr), closeErr); err != nil {
		return err
	}
	if len(entries) > maxEntries {
		return fmt.Errorf("export directory entry count exceeded")
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, entry := range entries {
		if err := w.exportPath(name + "/" + entry.Name()); err != nil {
			return err
		}
	}
	return nil
}

func nonEOF(err error) error {
	if errors.Is(err, io.EOF) {
		return nil
	}
	return err
}

func (w *guestExportWriter) exportFile(name string, info os.FileInfo) error {
	fresh, err := w.admitName(name)
	if err != nil || !fresh {
		return err
	}
	if w.files >= w.limits.maxFiles || info.Size() < 0 || info.Size() > w.limits.maxFile || info.Size() > w.limits.maxTotal-w.total {
		return fmt.Errorf("export file size or count exceeded")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Nlink != 1 {
		return fmt.Errorf("export file has unsupported link count")
	}
	file, err := w.root.OpenFile(name, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) || !opened.Mode().IsRegular() || opened.Size() != info.Size() {
		return fmt.Errorf("export file changed while opening: %v", err)
	}
	if err := writeFrame(w.output, 2, name, nil, uint64(info.Size()), [32]byte{}); err != nil {
		return err
	}
	hash := sha256.New()
	buffer := make([]byte, 1<<20)
	for remaining := info.Size(); remaining > 0; {
		if err := w.ctx.Err(); err != nil {
			return err
		}
		chunk := buffer
		if remaining < int64(len(chunk)) {
			chunk = chunk[:remaining]
		}
		if _, err := io.ReadFull(file, chunk); err != nil {
			return err
		}
		if err := writeFrame(w.output, 3, "", chunk, 0, [32]byte{}); err != nil {
			return err
		}
		_, _ = hash.Write(chunk)
		remaining -= int64(len(chunk))
	}
	final, err := file.Stat()
	if err != nil || !os.SameFile(info, final) || final.Size() != info.Size() {
		return fmt.Errorf("export file changed during read: %v", err)
	}
	var digest [32]byte
	copy(digest[:], hash.Sum(nil))
	if err := writeFrame(w.output, 4, "", nil, 0, digest); err != nil {
		return err
	}
	w.files++
	w.total += info.Size()
	return nil
}

func (w *guestExportWriter) safeInfo(name string) (os.FileInfo, error) {
	for _, part := range prefixes(name) {
		info, err := w.root.Lstat(part)
		if err != nil || info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("export path contains a missing or linked component: %v", err)
		}
		if part != name && !info.IsDir() {
			return nil, fmt.Errorf("export path parent is not a directory")
		}
		if part == name {
			return info, nil
		}
	}
	return nil, fmt.Errorf("empty export path")
}

func (w *guestExportWriter) admitName(name string) (bool, error) {
	folded := strings.ToLower(name)
	if previous, exists := w.seen[folded]; exists {
		if previous != name {
			return false, fmt.Errorf("case-colliding export paths")
		}
		return false, nil
	}
	w.seen[folded] = name
	return true, nil
}

func prefixes(name string) []string {
	parts := strings.Split(name, "/")
	result := make([]string, 0, len(parts))
	for n := range parts {
		result = append(result, strings.Join(parts[:n+1], "/"))
	}
	return result
}

func validGuestExportPath(name string) bool {
	if len(name) == 0 || len(name) > 512 || strings.HasPrefix(name, "/") || strings.HasSuffix(name, "/") {
		return false
	}
	parts := strings.Split(name, "/")
	if len(parts) > 16 {
		return false
	}
	for _, part := range parts {
		if part == "" || part == "." || part == ".." || len(part) > 64 {
			return false
		}
		for i := range part {
			c := part[i]
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '.' || c == '_' || c == '-') {
				return false
			}
		}
	}
	return true
}
