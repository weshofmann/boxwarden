package session

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

var (
	sessionSyncRoot        = syncSessionRootDirectory
	sessionBeforeOpenChild = func(*os.Root, string) {}
)

func openSessionStateRoot(path string) (*os.Root, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if err := requirePrivateDirectoryInfo(info); err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		return nil, err
	}
	opened, err := root.Stat(".")
	if err != nil {
		root.Close()
		return nil, err
	}
	if !os.SameFile(info, opened) {
		root.Close()
		return nil, fmt.Errorf("changed while opening")
	}
	return root, nil
}

func openSessionChild(parent *os.Root, name string, create bool) (*os.Root, error) {
	info, err := parent.Lstat(name)
	if errors.Is(err, os.ErrNotExist) && create {
		if err := parent.Mkdir(name, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		if err := sessionSyncRoot(parent); err != nil {
			return nil, fmt.Errorf("sync parent after create: %w", err)
		}
		info, err = parent.Lstat(name)
	}
	if err != nil {
		return nil, err
	}
	if err := requirePrivateDirectoryInfo(info); err != nil {
		return nil, err
	}
	sessionBeforeOpenChild(parent, name)
	child, err := parent.OpenRoot(name)
	if err != nil {
		return nil, err
	}
	opened, err := child.Stat(".")
	if err != nil {
		child.Close()
		return nil, err
	}
	if !os.SameFile(info, opened) {
		child.Close()
		return nil, fmt.Errorf("changed while opening")
	}
	return child, nil
}

func openSessionPrivateRegular(root *os.Root, name string) (*os.File, error) {
	info, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if err := requirePrivateRegularInfo(info); err != nil {
		return nil, err
	}
	file, err := root.OpenFile(name, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	opened, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, err
	}
	if err := requirePrivateRegularInfo(opened); err != nil {
		file.Close()
		return nil, err
	}
	if !os.SameFile(info, opened) {
		file.Close()
		return nil, fmt.Errorf("changed while opening")
	}
	return file, nil
}

func syncSessionRootDirectory(root *os.Root) error {
	directory, err := root.Open(".")
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

func requirePrivateDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	return requirePrivateDirectoryInfo(info)
}

func requirePrivateDirectoryInfo(info os.FileInfo) error {
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("must be a non-symlink directory")
	}
	if info.Mode().Perm() != 0o700 {
		return fmt.Errorf("must have mode 0700")
	}
	return nil
}

func requirePrivateRegularInfo(info os.FileInfo) error {
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("must be a regular non-symlink file")
	}
	if info.Mode().Perm() != 0o600 {
		return fmt.Errorf("must have mode 0600")
	}
	return nil
}
