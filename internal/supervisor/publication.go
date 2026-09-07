package supervisor

import (
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
		if err := admitRequestOnly(path, request); err != nil {
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

func admitRequestOnly(path string, want LaunchRequest) error {
	if !privateDirectory(filepath.Dir(path)) {
		return fmt.Errorf("generation directory is not owner-private")
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		return err
	}
	if len(entries) != 1 || entries[0].Name() != requestName || entries[0].IsDir() {
		return fmt.Errorf("generation is not request-only exact state")
	}
	got, artifact, err := admitLaunchRequest(path)
	if artifact != nil {
		defer artifact.close()
	}
	if err != nil || !launchRequestsEqual(got, want) {
		return fmt.Errorf("generation request does not match exact binding: %w", err)
	}
	return nil
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
