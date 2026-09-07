package supervisor

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
)

// publishOrAdmitRequest is the supervisor subsystem's sole outer-generation
// namespace entry point. A first launch stages a complete immutable request
// before publication; a retry admits only the request-only exact state.
func publishOrAdmitRequest(request LaunchRequest) (string, bool, error) {
	if err := validLaunchRequest(request); err != nil {
		return "", false, err
	}
	if request.SessionRecordName == "" || !validPart(request.SessionRecordName) {
		return "", false, fmt.Errorf("canonical session record name is required")
	}
	if filepath.Base(request.RuntimeDirectory) != request.Binding.Generation || filepath.Base(filepath.Dir(request.RuntimeDirectory)) != request.Binding.SessionID || filepath.Base(filepath.Dir(filepath.Dir(request.RuntimeDirectory))) != request.Binding.Domain {
		return "", false, fmt.Errorf("runtime directory does not match exact binding")
	}
	parent := filepath.Dir(request.RuntimeDirectory)
	if err := ensurePrivateParents(parent); err != nil {
		return "", false, err
	}
	for _, required := range []string{parent, filepath.Dir(parent), filepath.Dir(filepath.Dir(parent))} {
		if !privateDirectory(required) {
			return "", false, fmt.Errorf("runtime/domain/session parent is not owner-private")
		}
	}
	path := filepath.Join(request.RuntimeDirectory, requestName)
	if _, err := os.Lstat(request.RuntimeDirectory); err == nil {
		if err := admitOrCompleteBoundGeneration(path, request); err != nil {
			return "", false, err
		}
		return path, false, nil
	} else if !os.IsNotExist(err) {
		return "", false, err
	}
	stage, err := os.MkdirTemp(parent, "."+request.Binding.Generation+".stage-")
	if err != nil {
		return "", false, err
	}
	stageIdentity, err := capturePrivateDirectory(stage)
	if err != nil {
		return "", false, err
	}
	failed := true
	defer func() {
		if failed {
			if lockIdentity, err := capturePrivateRegular(filepath.Join(stage, lockName)); err == nil {
				_ = removeExact(lockIdentity, false)
			}
			if requestIdentity, err := capturePrivateRegular(filepath.Join(stage, requestName)); err == nil {
				_ = removeExact(requestIdentity, false)
			}
			_ = removeExactDirectory(stageIdentity)
		}
	}()
	if err := os.Chmod(stage, 0o700); err != nil {
		return "", false, err
	}
	if err := writeLaunchRequestAt(filepath.Join(stage, requestName), request); err != nil {
		return "", false, err
	}
	if err := writeBoundGenerationLockAt(filepath.Join(stage, lockName), request); err != nil {
		return "", false, err
	}
	if err := syncDirectory(stage); err != nil {
		return "", false, err
	}
	// The parent is session-operation locked. Never replace an entry observed
	// here; a concurrent appearance is drift and leaves both namespaces intact.
	if _, err := os.Lstat(request.RuntimeDirectory); !os.IsNotExist(err) {
		if err == nil {
			return "", false, fmt.Errorf("generation appeared during publication")
		}
		return "", false, err
	}
	if err := renameWithoutReplace(stage, request.RuntimeDirectory); err != nil {
		return "", false, err
	}
	if err := syncDirectory(parent); err != nil {
		return "", false, err
	}
	failed = false
	return path, true, nil
}

func admitOrCompleteBoundGeneration(path string, want LaunchRequest) error {
	runtime := filepath.Dir(path)
	runtimeIdentity, err := capturePrivateDirectory(runtime)
	if err != nil {
		return fmt.Errorf("generation directory is not owner-private")
	}
	entries, err := os.ReadDir(runtime)
	if err != nil {
		return err
	}
	requestOnly := len(entries) == 1 && entries[0].Name() == requestName && !entries[0].IsDir()
	if !requestOnly && !exactBoundGenerationEntries(entries) {
		return fmt.Errorf("generation is not exact bound request state")
	}
	got, artifact, err := admitLaunchRequest(path)
	if artifact != nil {
		defer artifact.close()
	}
	if err != nil || !launchRequestsEqual(got, want) {
		return fmt.Errorf("generation request does not match exact binding: %w", err)
	}
	if requestOnly {
		if err := publishBoundGenerationLock(filepath.Join(runtime, lockName), want); err != nil {
			return err
		}
	}
	if err := identityStillMatches(runtimeIdentity, func(info os.FileInfo) bool {
		return info.IsDir() && info.Mode().Perm() == 0o700 && ownedByCurrentUser(info)
	}); err != nil {
		return fmt.Errorf("generation directory changed during bound admission: %w", err)
	}
	if err := identityStillMatches(artifact.identity, func(info os.FileInfo) bool {
		return info.Mode().IsRegular() && info.Mode().Perm() == 0o600 && ownedByCurrentUser(info)
	}); err != nil {
		return fmt.Errorf("generation request changed during bound admission: %w", err)
	}
	entries, err = os.ReadDir(runtime)
	if err != nil {
		return err
	}
	if !exactBoundGenerationEntries(entries) {
		return fmt.Errorf("generation changed during bound admission")
	}
	lock, err := admitBoundGenerationLock(filepath.Join(runtime, lockName), want)
	if lock != nil {
		defer lock.close()
	}
	if err != nil {
		return fmt.Errorf("generation lock does not match exact request: %w", err)
	}
	return nil
}

func exactBoundGenerationEntries(entries []os.DirEntry) bool {
	return len(entries) == 2 && entries[0].Name() == lockName && !entries[0].IsDir() && entries[1].Name() == requestName && !entries[1].IsDir()
}

func generationIsRequestOnly(runtime string) bool {
	if !privateDirectory(runtime) {
		return false
	}
	entries, err := os.ReadDir(runtime)
	return err == nil && len(entries) == 1 && entries[0].Name() == requestName && !entries[0].IsDir()
}

// publishBoundGenerationLock performs the only retry mutation: an O_EXCL
// publication of the exact request-bound lock. A concurrent pre-existing lock
// is accepted only by the same strict admission used for steady state.
func publishBoundGenerationLock(path string, request LaunchRequest) error {
	err := writeBoundGenerationLock(path, request)
	if err == nil {
		return nil
	}
	if !errors.Is(err, os.ErrExist) {
		return err
	}
	lock, admitErr := admitBoundGenerationLock(path, request)
	if lock != nil {
		admitErr = errors.Join(admitErr, lock.close())
	}
	return admitErr
}

func launchRequestsEqual(got, want LaunchRequest) bool { return reflect.DeepEqual(got, want) }

func ensurePrivateParents(path string) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || path == "/" {
		return fmt.Errorf("generation parent must be canonical and absolute")
	}
	if _, err := os.Lstat(path); err == nil {
		if !privateDirectory(path) {
			return fmt.Errorf("generation parent is not owner-private")
		}
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	parent := filepath.Dir(path)
	if err := ensurePrivateParents(parent); err != nil {
		return err
	}
	if err := os.Mkdir(path, 0o700); err != nil && !os.IsExist(err) {
		return err
	}
	if !privateDirectory(path) {
		return fmt.Errorf("created generation parent is not owner-private")
	}
	return syncDirectory(parent)
}

func syncDirectory(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
