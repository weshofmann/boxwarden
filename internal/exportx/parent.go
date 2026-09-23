package exportx

import (
	"fmt"
	"os"
	"syscall"
)

func openPrivateParent(name string) (*os.File, *os.Root, os.FileInfo, error) {
	info, err := os.Lstat(name)
	if err != nil {
		return nil, nil, nil, err
	}
	if err := privateParentInfo(info); err != nil {
		return nil, nil, nil, err
	}
	file, err := os.Open(name)
	if err != nil {
		return nil, nil, nil, err
	}
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) {
		file.Close()
		return nil, nil, nil, fmt.Errorf("export parent changed while opening: %v", err)
	}
	root, err := os.OpenRoot(name)
	if err != nil {
		file.Close()
		return nil, nil, nil, err
	}
	rootInfo, err := root.Stat(".")
	if err != nil || !os.SameFile(info, rootInfo) {
		root.Close()
		file.Close()
		return nil, nil, nil, fmt.Errorf("export parent changed while opening root: %v", err)
	}
	return file, root, info, nil
}

func samePrivateParent(name string, original os.FileInfo, open *os.File) error {
	info, err := os.Lstat(name)
	if err != nil {
		return err
	}
	if err := privateParentInfo(info); err != nil {
		return err
	}
	opened, err := open.Stat()
	if err != nil {
		return err
	}
	if !os.SameFile(original, info) || !os.SameFile(original, opened) {
		return fmt.Errorf("export parent changed")
	}
	return nil
}

func privateParentInfo(info os.FileInfo) error {
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() || info.Mode().Perm() != 0o700 {
		return fmt.Errorf("export parent must be a non-symlink mode 0700 directory")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Geteuid()) {
		return fmt.Errorf("export parent must be owned by receiver")
	}
	return nil
}

func checkSpace(parent *os.File, reserve, incoming uint64) error {
	var stat syscall.Statfs_t
	if err := syscall.Fstatfs(int(parent.Fd()), &stat); err != nil {
		return err
	}
	if stat.Bsize <= 0 {
		return fmt.Errorf("invalid export filesystem block size")
	}
	blocks := uint64(stat.Bavail)
	blockSize := uint64(stat.Bsize)
	available := blocks * blockSize
	if blocks != 0 && available/blockSize != blocks {
		available = ^uint64(0)
	}
	if available < incoming || available-incoming < reserve {
		return fmt.Errorf("export filesystem free space below reserve")
	}
	return nil
}
