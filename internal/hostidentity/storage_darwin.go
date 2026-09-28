//go:build darwin && cgo

package hostidentity

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"github.com/weshofmann/boxwarden/internal/privateacl"
)

func checkStorage(expected StorageExpectation) error {
	for _, path := range []string{expected.ConfigPath, expected.StateRoot, expected.MountPoint} {
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil || resolved != path {
			return fmt.Errorf("workspace storage path is unavailable or has a symlink: %q: %v", path, err)
		}
	}
	configInfo, err := os.Lstat(expected.ConfigPath)
	if err != nil {
		return fmt.Errorf("external workspace storage config is unavailable: %w", err)
	}
	if err := privateStorageFile(configInfo); err != nil {
		return err
	}
	if err := privateacl.Check(expected.ConfigPath, configInfo, privateacl.OSInspector{}); err != nil {
		return fmt.Errorf("external workspace storage config ACL: %w", err)
	}
	config, err := os.OpenFile(expected.ConfigPath, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return fmt.Errorf("open external workspace storage config: %w", err)
	}
	defer config.Close()
	openedConfig, err := config.Stat()
	if err != nil || !os.SameFile(configInfo, openedConfig) {
		return fmt.Errorf("external workspace storage config changed: %v", err)
	}
	if err := privateStorageFile(openedConfig); err != nil {
		return err
	}
	if err := privateacl.Check(expected.ConfigPath, openedConfig, privateacl.OSInspector{}); err != nil {
		return fmt.Errorf("external workspace storage config ACL changed: %w", err)
	}
	var configFS syscall.Statfs_t
	if err := syscall.Fstatfs(int(config.Fd()), &configFS); err != nil {
		return err
	}

	rootInfo, err := os.Lstat(expected.StateRoot)
	if err != nil {
		return fmt.Errorf("workspace backing state root is unavailable: %w", err)
	}
	if err := privateStorageDirectory(rootInfo); err != nil {
		return err
	}
	if err := privateacl.Check(expected.StateRoot, rootInfo, privateacl.OSInspector{}); err != nil {
		return fmt.Errorf("workspace backing state root ACL: %w", err)
	}
	root, err := os.OpenFile(expected.StateRoot, os.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return fmt.Errorf("open workspace backing state root: %w", err)
	}
	defer root.Close()
	openedRoot, err := root.Stat()
	if err != nil || !os.SameFile(rootInfo, openedRoot) {
		return fmt.Errorf("workspace backing state root changed: %v", err)
	}
	if err := privateStorageDirectory(openedRoot); err != nil {
		return err
	}
	if err := privateacl.Check(expected.StateRoot, openedRoot, privateacl.OSInspector{}); err != nil {
		return fmt.Errorf("workspace backing root ACL changed: %w", err)
	}
	identity, err := Observe(root)
	if err != nil {
		return fmt.Errorf("observe workspace backing storage: %w", err)
	}
	var backingFS syscall.Statfs_t
	if err := syscall.Fstatfs(int(root.Fd()), &backingFS); err != nil {
		return err
	}
	if err := validateStorageObservations(expected, configFS.Fsid, backingFS.Fsid, int8String(backingFS.Mntonname[:]), identity.VolumeUUID); err != nil {
		return err
	}
	mountInfo, err := os.Lstat(expected.MountPoint)
	if err != nil || !mountInfo.IsDir() || mountInfo.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("expected workspace mount is unavailable or unsafe: %v", err)
	}
	mount, err := os.OpenFile(expected.MountPoint, os.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	defer mount.Close()
	openedMount, err := mount.Stat()
	if err != nil || !os.SameFile(mountInfo, openedMount) {
		return fmt.Errorf("expected workspace mount changed: %v", err)
	}
	var mountFS syscall.Statfs_t
	if err := syscall.Fstatfs(int(mount.Fd()), &mountFS); err != nil {
		return err
	}
	if mountFS.Fsid != backingFS.Fsid {
		return fmt.Errorf("workspace state root does not reside on expected mounted filesystem")
	}
	return nil
}

func privateStorageFile(info os.FileInfo) error {
	if info == nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		return fmt.Errorf("workspace config must be a private regular file mode 0600")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Nlink != 1 || int(stat.Uid) != os.Getuid() {
		return fmt.Errorf("workspace config must have one link and current operator owner")
	}
	return nil
}

func privateStorageDirectory(info os.FileInfo) error {
	if info == nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() || info.Mode().Perm() != 0o700 {
		return fmt.Errorf("workspace backing root must be a private directory mode 0700")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(stat.Uid) != os.Getuid() {
		return fmt.Errorf("workspace backing root owner differs from current operator")
	}
	return nil
}

func validateStorageObservations(expected StorageExpectation, configFS, backingFS syscall.Fsid, mountPoint, volumeUUID string) error {
	if configFS == backingFS {
		return fmt.Errorf("workspace storage config is on the backing filesystem")
	}
	if mountPoint != expected.MountPoint {
		return fmt.Errorf("workspace backing mount changed: got %q, expected %q", mountPoint, expected.MountPoint)
	}
	if volumeUUID != expected.VolumeUUID {
		return fmt.Errorf("workspace APFS volume UUID changed: got %q, expected %q", volumeUUID, expected.VolumeUUID)
	}
	return nil
}
