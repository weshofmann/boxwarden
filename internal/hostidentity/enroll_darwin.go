//go:build darwin && cgo

package hostidentity

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"github.com/weshofmann/boxwarden/internal/privateacl"
)

func writeEnrolledConfig(expected StorageExpectation, data []byte) error {
	parentPath := filepath.Dir(expected.ConfigPath)
	for _, path := range []string{parentPath, expected.StateRoot, expected.MountPoint} {
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil || resolved != path {
			return fmt.Errorf("enrollment path is unavailable or has a symlink: %q: %v", path, err)
		}
	}
	if _, err := os.Lstat(expected.ConfigPath); !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("enrolled config target must not exist: %v", err)
	}
	parent, parentInfo, err := openPrivateEnrollmentDirectory(parentPath)
	if err != nil {
		return fmt.Errorf("open external configuration parent: %w", err)
	}
	defer parent.Close()
	root, _, err := openPrivateEnrollmentDirectory(expected.StateRoot)
	if err != nil {
		return fmt.Errorf("open workspace backing root: %w", err)
	}
	defer root.Close()
	var parentFS, rootFS syscall.Statfs_t
	if err := syscall.Fstatfs(int(parent.Fd()), &parentFS); err != nil {
		return err
	}
	if err := syscall.Fstatfs(int(root.Fd()), &rootFS); err != nil {
		return err
	}
	if parentFS.Fsid == rootFS.Fsid {
		return fmt.Errorf("enrolled config target is on the backing filesystem")
	}
	identity, err := Observe(root)
	if err != nil {
		return fmt.Errorf("observe workspace backing APFS identity: %w", err)
	}
	if err := validateEnrollmentObservations(expected, parentFS.Fsid, rootFS.Fsid, int8String(rootFS.Mntonname[:]), identity.VolumeUUID); err != nil {
		return err
	}
	mount, _, err := openPrivateEnrollmentMount(expected.MountPoint)
	if err != nil {
		return err
	}
	defer mount.Close()
	var mountFS syscall.Statfs_t
	if err := syscall.Fstatfs(int(mount.Fd()), &mountFS); err != nil {
		return err
	}
	if mountFS.Fsid != rootFS.Fsid {
		return fmt.Errorf("workspace state root does not reside on expected mounted filesystem")
	}
	container, err := os.OpenRoot(parentPath)
	if err != nil {
		return err
	}
	defer container.Close()
	openedParent, err := container.Stat(".")
	if err != nil || !os.SameFile(parentInfo, openedParent) {
		return fmt.Errorf("configuration parent changed before publication: %v", err)
	}
	name := filepath.Base(expected.ConfigPath)
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return err
	}
	temporaryName := "." + name + ".tmp-" + hex.EncodeToString(nonce[:])
	temporary, err := container.OpenFile(temporaryName, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return err
	}
	defer container.Remove(temporaryName)
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	// Link is atomic and refuses an existing target. Removing the temporary
	// link leaves the published authority with exactly one link.
	if err := container.Link(temporaryName, name); err != nil {
		return err
	}
	if err := container.Remove(temporaryName); err != nil {
		return err
	}
	if err := syncEnrollmentDirectory(parent); err != nil {
		return err
	}
	if err := CheckStorage(expected); err != nil {
		return fmt.Errorf("recheck published workspace enrollment: %w", err)
	}
	return nil
}

func validateEnrollmentObservations(expected StorageExpectation, outputFS, backingFS syscall.Fsid, mountPoint, volumeUUID string) error {
	return validateStorageObservations(expected, outputFS, backingFS, mountPoint, volumeUUID)
}

func openPrivateEnrollmentDirectory(path string) (*os.File, os.FileInfo, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, nil, err
	}
	if err := privateStorageDirectory(info); err != nil {
		return nil, nil, err
	}
	if err := privateacl.Check(path, info, privateacl.OSInspector{}); err != nil {
		return nil, nil, err
	}
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, nil, err
	}
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) {
		file.Close()
		return nil, nil, fmt.Errorf("private enrollment directory changed: %v", err)
	}
	if err := privateStorageDirectory(opened); err != nil {
		file.Close()
		return nil, nil, err
	}
	if err := privateacl.Check(path, opened, privateacl.OSInspector{}); err != nil {
		file.Close()
		return nil, nil, err
	}
	return file, opened, nil
}

func openPrivateEnrollmentMount(path string) (*os.File, os.FileInfo, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, nil, fmt.Errorf("expected enrollment mount is unavailable or unsafe: %v", err)
	}
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, nil, err
	}
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) {
		file.Close()
		return nil, nil, fmt.Errorf("expected enrollment mount changed: %v", err)
	}
	return file, opened, nil
}

func syncEnrollmentDirectory(directory *os.File) error {
	return directory.Sync()
}
