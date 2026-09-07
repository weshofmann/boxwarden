package supervisor

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

var errGenerationAlreadyOwned = errors.New("exact generation is already owned")

type exactGenerationState uint8

const (
	exactGenerationAbsent exactGenerationState = iota
	exactGenerationRequestOnly
	exactGenerationResumable
	exactGenerationLive
)

// publishOrAdmitRequest is called under the common session operation lock.
// Stage a complete request and ordinary empty lock before publishing the
// generation. Never create serial/: that subtree belongs exclusively to serialx.
func publishOrAdmitRequest(r LaunchRequest) (string, bool, error) {
	if err := validLaunchRequest(r); err != nil {
		return "", false, err
	}
	parent := filepath.Dir(r.RuntimeDirectory)
	if err := ensurePrivateParents(parent); err != nil {
		return "", false, err
	}
	for _, p := range []string{parent, filepath.Dir(parent), filepath.Dir(filepath.Dir(parent))} {
		if !privateDirectory(p) {
			return "", false, fmt.Errorf("runtime/domain/session parent is not private")
		}
	}
	path := filepath.Join(r.RuntimeDirectory, requestName)
	state, err := classifyExactGeneration(r)
	if err != nil {
		return "", false, err
	}
	if state != exactGenerationAbsent {
		if state == exactGenerationRequestOnly {
			if err := writePrivateFile(filepath.Join(r.RuntimeDirectory, lockName), nil); err != nil && !os.IsExist(err) {
				return "", false, err
			}
			if _, err := classifyExactGeneration(r); err != nil {
				return "", false, err
			}
			if err := syncDirectory(r.RuntimeDirectory); err != nil {
				return "", false, err
			}
		}
		return path, false, nil
	}
	stage, err := os.MkdirTemp(parent, "."+r.Binding.Generation+".stage-")
	if err != nil {
		return "", false, err
	}
	defer os.RemoveAll(stage)
	data, err := json.Marshal(r)
	if err != nil {
		return "", false, err
	}
	if err := writePrivateFile(filepath.Join(stage, requestName), data); err != nil {
		return "", false, err
	}
	if err := writePrivateFile(filepath.Join(stage, lockName), nil); err != nil {
		return "", false, err
	}
	if err := syncDirectory(stage); err != nil {
		return "", false, err
	}
	// Cooperating callers hold the session lock; reject pre-existing targets.
	if _, err := os.Lstat(r.RuntimeDirectory); !os.IsNotExist(err) {
		return "", false, fmt.Errorf("generation appeared during publication")
	}
	if err := os.Rename(stage, r.RuntimeDirectory); err != nil {
		return "", false, err
	}
	if err := syncDirectory(parent); err != nil {
		return "", false, err
	}
	return path, true, nil
}
func writePrivateFile(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, writeErr := f.Write(data)
	return errors.Join(writeErr, f.Sync(), f.Close())
}
func ensurePrivateParents(path string) error {
	if !canonicalAbsolute(path) {
		return fmt.Errorf("invalid private parent")
	}
	if _, err := os.Lstat(path); err == nil {
		if !privateDirectory(path) {
			return fmt.Errorf("parent is not owner-private")
		}
		return safeParents(path)
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := ensurePrivateParents(filepath.Dir(path)); err != nil {
		return err
	}
	if err := os.Mkdir(path, 0700); err != nil && !os.IsExist(err) {
		return err
	}
	if !privateDirectory(path) {
		return fmt.Errorf("created parent is not private")
	}
	return syncDirectory(filepath.Dir(path))
}
func syncDirectory(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}
func admitGeneration(r LaunchRequest) (bool, error) {
	if err := validLaunchRequest(r); err != nil {
		return false, err
	}
	if err := safeParents(r.RuntimeDirectory); err != nil {
		return false, err
	}
	if !privateDirectory(r.RuntimeDirectory) {
		return false, fmt.Errorf("generation is not owner-private")
	}
	got, err := readLaunchRequest(filepath.Join(r.RuntimeDirectory, requestName))
	if err != nil || got != r {
		return false, fmt.Errorf("generation request is missing, malformed or foreign: %w", err)
	}
	entries, err := os.ReadDir(r.RuntimeDirectory)
	if err != nil {
		return false, err
	}
	later := false
	for _, entry := range entries {
		if err := validateGenerationEntry(r.RuntimeDirectory, entry.Name()); err != nil {
			return false, err
		}
		if entry.Name() != requestName && entry.Name() != lockName {
			later = true
		}
	}
	return later, nil
}
func validateGenerationEntry(dir, name string) error {
	info, err := os.Lstat(filepath.Join(dir, name))
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !ownedByCurrentUser(info) {
		return fmt.Errorf("unsafe generation entry %q", name)
	}
	switch name {
	case requestName, lockName, "client", "known_hosts":
		if !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
			return fmt.Errorf("invalid private file %q", name)
		}
		if name == lockName && info.Size() != 0 {
			return fmt.Errorf("generation lock must be empty")
		}
	case "client.pub", "client-cert.pub":
		if !info.Mode().IsRegular() || info.Mode().Perm() != 0644 {
			return fmt.Errorf("invalid public file %q", name)
		}
	case "serial":
		if !info.IsDir() || info.Mode().Perm() != 0700 {
			return fmt.Errorf("invalid serial subtree")
		}
	case socketName:
		if info.Mode()&os.ModeSocket == 0 || info.Mode().Perm() != 0600 {
			return fmt.Errorf("invalid control socket")
		}
	default:
		return fmt.Errorf("unexpected generation entry %q", name)
	}
	return nil
}
func classifyExactGeneration(r LaunchRequest) (exactGenerationState, error) {
	if err := validLaunchRequest(r); err != nil {
		return 0, err
	}
	if _, err := os.Lstat(r.RuntimeDirectory); os.IsNotExist(err) {
		return exactGenerationAbsent, nil
	} else if err != nil {
		return 0, err
	}
	later, err := admitGeneration(r)
	if err != nil {
		return 0, err
	}
	lockPath := filepath.Join(r.RuntimeDirectory, lockName)
	if _, err := os.Lstat(lockPath); os.IsNotExist(err) {
		if later {
			return 0, fmt.Errorf("live artifacts without generation lock")
		}
		return exactGenerationRequestOnly, nil
	}
	f, err := acquireGenerationLock(r)
	if errors.Is(err, errGenerationAlreadyOwned) {
		return exactGenerationLive, nil
	}
	if err != nil {
		return 0, err
	}
	defer f.Close()
	if later {
		return 0, fmt.Errorf("stale unheld live artifacts require reconciliation")
	}
	return exactGenerationResumable, nil
}

// removeExactGeneration removes only the supervisor-owned outer namespace.
// RuntimeOwner must finish serial and other runtime cleanup before this runs.
// Validate the complete directory before unlinking anything and never recurse.
func removeExactGeneration(r LaunchRequest) error {
	if _, err := admitGeneration(r); err != nil {
		return fmt.Errorf("admit exact generation for cleanup: %w", err)
	}
	entries, err := os.ReadDir(r.RuntimeDirectory)
	if err != nil {
		return err
	}
	foundRequest := false
	foundLock := false
	for _, entry := range entries {
		if err := validateGenerationEntry(r.RuntimeDirectory, entry.Name()); err != nil {
			return err
		}
		switch entry.Name() {
		case requestName:
			foundRequest = true
		case lockName:
			foundLock = true
		case socketName:
		default:
			return fmt.Errorf("unexpected generation entry %q during cleanup", entry.Name())
		}
	}
	if !foundRequest || !foundLock {
		return fmt.Errorf("exact generation cleanup requires request and lock")
	}
	for _, name := range []string{socketName, requestName, lockName} {
		path := filepath.Join(r.RuntimeDirectory, name)
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	if err := os.Remove(r.RuntimeDirectory); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(r.RuntimeDirectory))
}

// The descriptor is the lifetime ownership lock, never persisted process evidence.
func acquireGenerationLock(r LaunchRequest) (*os.File, error) {
	path := filepath.Join(r.RuntimeDirectory, lockName)
	if err := validateGenerationEntry(r.RuntimeDirectory, lockName); err != nil {
		return nil, err
	}
	f, err := openPrivateFile(path)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			return nil, errGenerationAlreadyOwned
		}
		return nil, err
	}
	return f, nil
}
