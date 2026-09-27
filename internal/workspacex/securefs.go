package workspacex

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"github.com/weshofmann/boxwarden/internal/privateacl"
)

var aclInspector privateacl.Inspector = privateacl.OSInspector{}

func checkPrivateACL(path string, expected os.FileInfo) error {
	return privateacl.Check(path, expected, aclInspector)
}

func openStateRoot(path string) (*os.Root, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, fmt.Errorf("state root must be a clean absolute path")
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if err := privateDirectory(info); err != nil {
		return nil, err
	}
	if err := checkPrivateACL(path, info); err != nil {
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
		return nil, fmt.Errorf("state root changed while opening")
	}
	if err := privateDirectory(opened); err != nil {
		root.Close()
		return nil, err
	}
	if err := checkPrivateACL(path, opened); err != nil {
		root.Close()
		return nil, err
	}
	return root, nil
}

func openChild(root *os.Root, name string, create bool) (*os.Root, error) {
	info, err := root.Lstat(name)
	if errors.Is(err, os.ErrNotExist) && create {
		if err := root.Mkdir(name, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		if err := syncDirectory(root); err != nil {
			return nil, err
		}
		info, err = root.Lstat(name)
	}
	if err != nil {
		return nil, err
	}
	if err := privateDirectory(info); err != nil {
		return nil, err
	}
	path := filepath.Join(root.Name(), name)
	if err := checkPrivateACL(path, info); err != nil {
		return nil, err
	}
	child, err := root.OpenRoot(name)
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
		return nil, fmt.Errorf("%s changed while opening", name)
	}
	if err := privateDirectory(opened); err != nil {
		child.Close()
		return nil, err
	}
	if err := checkPrivateACL(path, opened); err != nil {
		child.Close()
		return nil, err
	}
	return child, nil
}

func openPrivateFile(root *os.Root, name string) (*os.File, error) {
	info, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if err := privateRegular(info); err != nil {
		return nil, err
	}
	path := filepath.Join(root.Name(), name)
	if err := checkPrivateACL(path, info); err != nil {
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
	if err := privateRegular(opened); err != nil {
		file.Close()
		return nil, err
	}
	if !os.SameFile(info, opened) {
		file.Close()
		return nil, fmt.Errorf("file changed while opening")
	}
	if err := checkPrivateACL(path, opened); err != nil {
		file.Close()
		return nil, err
	}
	return file, nil
}

func privateDirectory(info os.FileInfo) error {
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() || info.Mode().Perm() != 0o700 {
		return fmt.Errorf("must be a private non-symlink directory with mode 0700")
	}
	return ownedByOperator(info)
}

func privateRegular(info os.FileInfo) error {
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		return fmt.Errorf("must be a private non-symlink regular file with mode 0600")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Nlink != 1 {
		return fmt.Errorf("file must have one link")
	}
	return ownedByOperator(info)
}

func ownedByOperator(info os.FileInfo) error {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(stat.Uid) != os.Getuid() {
		return fmt.Errorf("path must be owned by current operator")
	}
	return nil
}

func diskIdentity(info os.FileInfo) (DiskIdentity, error) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Dev == 0 || stat.Ino == 0 {
		return DiskIdentity{}, fmt.Errorf("disk identity unavailable")
	}
	return DiskIdentity{Device: uint64(stat.Dev), Inode: uint64(stat.Ino)}, nil
}

func syncDirectory(root *os.Root) error {
	file, err := root.Open(".")
	if err != nil {
		return err
	}
	defer file.Close()
	return file.Sync()
}
