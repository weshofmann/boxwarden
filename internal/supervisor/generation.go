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

type exactCleanupResidue struct {
	directory, request, lock, marker bool
}

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
	if err := validatePrivateRuntimeParents(parent); err != nil {
		return "", false, err
	}
	canonicalExists, err := pathExists(r.RuntimeDirectory)
	if err != nil {
		return "", false, err
	}
	cleanup, err := admitExactCleanupResidue(r)
	if err != nil {
		return "", false, err
	}
	if canonicalExists && cleanup.present() {
		return "", false, fmt.Errorf("canonical generation and exact cleanup residue coexist")
	}
	if cleanup.present() {
		if err := finishExactGenerationCleanup(r, nil); err != nil {
			return "", false, err
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

func validatePrivateRuntimeParents(parent string) error {
	for _, p := range []string{parent, filepath.Dir(parent), filepath.Dir(filepath.Dir(parent))} {
		if !privateDirectory(p) {
			return fmt.Errorf("runtime/domain/session parent is not private")
		}
	}
	return nil
}

func pathExists(path string) (bool, error) {
	_, err := os.Lstat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
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
	canonicalExists, err := pathExists(r.RuntimeDirectory)
	if err != nil {
		return 0, err
	}
	cleanup, err := admitExactCleanupResidue(r)
	if err != nil {
		return 0, err
	}
	if canonicalExists && cleanup.present() {
		return 0, fmt.Errorf("canonical generation and exact cleanup residue coexist")
	}
	if !canonicalExists {
		return exactGenerationAbsent, nil
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

type generationCleanupOperations interface {
	Rename(string, string) error
	Remove(string) error
	SyncDirectory(string) error
}

type filesystemGenerationCleanupOperations struct{}

func (filesystemGenerationCleanupOperations) Rename(oldPath, newPath string) error {
	return os.Rename(oldPath, newPath)
}

func (filesystemGenerationCleanupOperations) Remove(path string) error {
	return os.Remove(path)
}

func (filesystemGenerationCleanupOperations) SyncDirectory(path string) error {
	return syncDirectory(path)
}

// removeExactGeneration removes only the supervisor-owned outer namespace.
// RuntimeOwner must finish serial and other runtime cleanup before this runs.
// The control listener must already have removed its exact socket; a residual
// socket is never authority for this generic cleanup to unlink it.
// Validate the complete directory before atomically moving it to the exact
// cleanup name. The caller still holds generation.lock across this transaction,
// so a same-G retry can contend on the moved inode rather than race cleanup.
// Every subsequent residue is exact and resumable; cleanup never recurses.
func removeExactGeneration(r LaunchRequest, retainedLock *os.File) error {
	return removeExactGenerationWithOperations(r, retainedLock, filesystemGenerationCleanupOperations{})
}

func removeExactGenerationWithOperations(r LaunchRequest, retainedLock *os.File, operations generationCleanupOperations) error {
	if retainedLock == nil || operations == nil {
		return fmt.Errorf("retained generation lock and cleanup operations are required")
	}
	if _, err := admitGeneration(r); err != nil {
		return fmt.Errorf("admit exact generation for cleanup: %w", err)
	}
	if err := validatePrivateRuntimeParents(filepath.Dir(r.RuntimeDirectory)); err != nil {
		return err
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
		default:
			return fmt.Errorf("unexpected generation entry %q during cleanup", entry.Name())
		}
	}
	if !foundRequest || !foundLock {
		return fmt.Errorf("exact generation cleanup requires request and lock")
	}
	lockPath := filepath.Join(r.RuntimeDirectory, lockName)
	lockInfo, err := retainedLock.Stat()
	if err != nil {
		return err
	}
	pathLockInfo, err := os.Lstat(lockPath)
	if err != nil {
		return err
	}
	if !os.SameFile(lockInfo, pathLockInfo) {
		return fmt.Errorf("retained generation lock does not match exact cleanup lock")
	}
	cleanup, err := admitExactCleanupResidue(r)
	if err != nil {
		return err
	}
	if cleanup.present() {
		return fmt.Errorf("exact cleanup residue already exists")
	}
	residue := exactCleanupDirectory(r)
	if err := operations.Rename(r.RuntimeDirectory, residue); err != nil {
		return err
	}
	parent := filepath.Dir(r.RuntimeDirectory)
	if err := operations.SyncDirectory(parent); err != nil {
		return err
	}
	return finishExactGenerationCleanupWithOperations(r, lockInfo, operations)
}

func (s exactCleanupResidue) present() bool {
	return s.directory || s.marker
}

func exactCleanupDirectory(r LaunchRequest) string {
	return filepath.Join(filepath.Dir(r.RuntimeDirectory), "."+r.Binding.Generation+".cleanup")
}

func exactCleanupLockMarker(r LaunchRequest) string {
	return exactCleanupDirectory(r) + ".lock"
}

func admitExactCleanupResidue(r LaunchRequest) (exactCleanupResidue, error) {
	var state exactCleanupResidue
	if err := validLaunchRequest(r); err != nil {
		return state, err
	}
	parent := filepath.Dir(r.RuntimeDirectory)
	parentExists, err := pathExists(parent)
	if err != nil {
		return state, err
	}
	if !parentExists {
		return state, nil
	}
	if err := safeParents(parent); err != nil {
		return state, err
	}
	if err := validatePrivateRuntimeParents(parent); err != nil {
		return state, err
	}
	directory := exactCleanupDirectory(r)
	marker := exactCleanupLockMarker(r)
	state.directory, err = pathExists(directory)
	if err != nil {
		return state, err
	}
	state.marker, err = pathExists(marker)
	if err != nil {
		return state, err
	}
	if state.marker {
		if err := validateCleanupLock(marker); err != nil {
			return state, err
		}
	}
	if !state.directory {
		return state, nil
	}
	if !privateDirectory(directory) {
		return state, fmt.Errorf("exact cleanup residue is not an owner-private directory")
	}
	if err := safeParents(directory); err != nil {
		return state, err
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return state, err
	}
	for _, entry := range entries {
		if err := validateGenerationEntry(directory, entry.Name()); err != nil {
			return state, err
		}
		switch entry.Name() {
		case requestName:
			got, err := readExactRequestFile(filepath.Join(directory, requestName))
			if err != nil || got != r {
				return state, fmt.Errorf("cleanup request is missing, malformed or foreign: %w", err)
			}
			state.request = true
		case lockName:
			state.lock = true
		default:
			return state, fmt.Errorf("unexpected exact cleanup entry %q", entry.Name())
		}
	}
	if !state.request && !state.lock && !state.marker {
		return state, fmt.Errorf("exact cleanup directory has no ownership lock")
	}
	if state.request && !state.lock && !state.marker {
		return state, fmt.Errorf("exact cleanup request exists without its lock")
	}
	if state.lock && state.marker {
		return state, fmt.Errorf("exact cleanup has two lock paths")
	}
	return state, nil
}

func validateCleanupLock(path string) error {
	if filepath.Base(path) == "" || !privateDirectory(filepath.Dir(path)) {
		return fmt.Errorf("unsafe exact cleanup lock path")
	}
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || info.Size() != 0 || !ownedByCurrentUser(info) {
		return fmt.Errorf("invalid exact cleanup lock")
	}
	return nil
}

func acquireCleanupLock(path string) (*os.File, error) {
	if err := validateCleanupLock(path); err != nil {
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

// finishExactGenerationCleanup admits and completes only the deterministic
// residue for r. ownedLock identifies the already-held lock inode used by Run;
// retry callers pass nil and must acquire the residue's reachable lock first.
func finishExactGenerationCleanup(r LaunchRequest, ownedLock os.FileInfo) error {
	return finishExactGenerationCleanupWithOperations(r, ownedLock, filesystemGenerationCleanupOperations{})
}

func finishExactGenerationCleanupWithOperations(r LaunchRequest, ownedLock os.FileInfo, operations generationCleanupOperations) error {
	if operations == nil {
		return fmt.Errorf("generation cleanup operations are unavailable")
	}
	canonicalExists, err := pathExists(r.RuntimeDirectory)
	if err != nil {
		return err
	}
	state, err := admitExactCleanupResidue(r)
	if err != nil {
		return err
	}
	if canonicalExists && state.present() {
		return fmt.Errorf("canonical generation and exact cleanup residue coexist")
	}
	if canonicalExists || !state.present() {
		return nil
	}

	directory := exactCleanupDirectory(r)
	marker := exactCleanupLockMarker(r)
	lockPath := ""
	if state.marker {
		lockPath = marker
	} else if state.lock {
		lockPath = filepath.Join(directory, lockName)
	}
	var acquired *os.File
	if lockPath != "" {
		if ownedLock == nil {
			acquired, err = acquireCleanupLock(lockPath)
			if err != nil {
				return err
			}
			defer acquired.Close()
			ownedLock, err = acquired.Stat()
			if err != nil {
				return err
			}
		} else {
			info, err := os.Lstat(lockPath)
			if err != nil || !os.SameFile(info, ownedLock) {
				return fmt.Errorf("cleanup lock does not match retained owner descriptor")
			}
		}
		state, err = admitExactCleanupResidue(r)
		if err != nil {
			return err
		}
	}
	if state.request {
		if err := operations.Remove(filepath.Join(directory, requestName)); err != nil {
			return err
		}
		state.request = false
		if err := operations.SyncDirectory(directory); err != nil {
			return err
		}
	}
	if state.lock {
		if state.marker {
			return fmt.Errorf("exact cleanup has two lock paths")
		}
		if err := operations.Rename(filepath.Join(directory, lockName), marker); err != nil {
			return err
		}
		state.lock = false
		state.marker = true
		if err := operations.SyncDirectory(directory); err != nil {
			return err
		}
		if err := operations.SyncDirectory(filepath.Dir(directory)); err != nil {
			return err
		}
	}
	if state.directory {
		entries, err := os.ReadDir(directory)
		if err != nil {
			return err
		}
		if len(entries) != 0 {
			return fmt.Errorf("exact cleanup directory is not empty")
		}
		if err := operations.Remove(directory); err != nil {
			return err
		}
		state.directory = false
		if err := operations.SyncDirectory(filepath.Dir(directory)); err != nil {
			return err
		}
	}
	if state.marker {
		info, err := os.Lstat(marker)
		if err != nil || ownedLock == nil || !os.SameFile(info, ownedLock) {
			return fmt.Errorf("cleanup lock marker does not match retained owner descriptor")
		}
		if err := operations.Remove(marker); err != nil {
			return err
		}
	}
	return operations.SyncDirectory(filepath.Dir(r.RuntimeDirectory))
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
