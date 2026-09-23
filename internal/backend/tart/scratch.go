package tart

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"syscall"

	"github.com/weshofmann/boxwarden/internal/backend"
)

// ErrScratchCleanupUnproven prevents the outer generation from being removed
// when Tart's exact scratch cannot be safely cleaned after its process reaps.
var ErrScratchCleanupUnproven = errors.New("Tart scratch cleanup is unproven")

const scratchName = "tart"

func createScratch(generation string) (string, os.FileInfo, error) {
	info, err := os.Lstat(generation)
	if err != nil {
		return "", nil, fmt.Errorf("exact generation is not an owner-private directory: %w", err)
	}
	if !privateScratchDirectory(info) {
		return "", nil, fmt.Errorf("exact generation is not an owner-private directory")
	}
	path := filepath.Join(generation, scratchName)
	// Exclusive creation refuses even an empty directory left by another run.
	if err := os.Mkdir(path, 0o700); err != nil {
		return "", nil, fmt.Errorf("create exclusive Tart scratch: %w", err)
	}
	scratchInfo, err := os.Lstat(path)
	if err != nil {
		return "", nil, fmt.Errorf("created Tart scratch is unsafe: %w", err)
	}
	if !privateScratchDirectory(scratchInfo) {
		return "", nil, fmt.Errorf("created Tart scratch is unsafe")
	}
	if err := syncScratchDirectory(generation); err != nil {
		return "", nil, errors.Join(err, cleanupScratch(path, scratchInfo))
	}
	return path, scratchInfo, nil
}

func privateScratchDirectory(info os.FileInfo) bool {
	if info == nil || !info.IsDir() || info.Mode().Perm() != 0o700 || info.Mode()&os.ModeSymlink != 0 {
		return false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && int(stat.Uid) == os.Getuid()
}

func cleanupScratch(path string, owned os.FileInfo) error {
	info, err := os.Lstat(path)
	if err != nil || !privateScratchDirectory(info) || owned == nil || !os.SameFile(info, owned) {
		return fmt.Errorf("%w: exact scratch directory changed or vanished: %v", ErrScratchCleanupUnproven, err)
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return fmt.Errorf("%w: read exact scratch: %v", ErrScratchCleanupUnproven, err)
	}
	if len(entries) > 1 || len(entries) == 1 && entries[0].Name() != "control.sock" {
		return fmt.Errorf("%w: unexpected scratch contents", ErrScratchCleanupUnproven)
	}
	if len(entries) == 1 {
		control := filepath.Join(path, "control.sock")
		info, err := os.Lstat(control)
		if err != nil || info.Mode()&os.ModeSocket == 0 || info.Mode().Perm() != 0o755 || !ownedScratchEntry(info) {
			return fmt.Errorf("%w: unsafe Tart control socket: %v", ErrScratchCleanupUnproven, err)
		}
		if err := os.Remove(control); err != nil {
			return fmt.Errorf("%w: remove Tart control socket: %v", ErrScratchCleanupUnproven, err)
		}
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("%w: remove exact scratch: %v", ErrScratchCleanupUnproven, err)
	}
	if err := syncScratchDirectory(filepath.Dir(path)); err != nil {
		return fmt.Errorf("%w: sync scratch parent: %v", ErrScratchCleanupUnproven, err)
	}
	return nil
}

func ownedScratchEntry(info os.FileInfo) bool {
	if info == nil || info.Mode()&os.ModeSymlink != 0 {
		return false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && int(stat.Uid) == os.Getuid()
}

func syncScratchDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	return errors.Join(directory.Sync(), directory.Close())
}

// scratchHandle defers cleanup until the retained Tart handle has actually
// reaped. A canceled Wait leaves the scratch intact for a later owner Wait.
type scratchHandle struct {
	backend.Handle
	path        string
	info        os.FileInfo
	cleanupOnce sync.Once
	cleanupErr  error
}

func (h *scratchHandle) Wait(ctx context.Context) error {
	err := h.Handle.Wait(ctx)
	if ctx.Err() != nil {
		return errors.Join(err, ctx.Err())
	}
	if errors.Is(err, ErrReapUnproven) {
		return err
	}
	h.cleanupOnce.Do(func() { h.cleanupErr = cleanupScratch(h.path, h.info) })
	return errors.Join(err, h.cleanupErr)
}
