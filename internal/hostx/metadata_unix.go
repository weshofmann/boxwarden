//go:build darwin || linux

package hostx

import (
	"fmt"
	"os"
	"syscall"
)

type fileIdentity struct {
	device uint64
	inode  uint64
	uid    int
}

func ownership(info os.FileInfo) (int, int, bool) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, 0, false
	}
	return int(stat.Uid), int(stat.Gid), true
}

func unixMode(info os.FileInfo) uint32 {
	mode := uint32(info.Mode().Perm())
	if info.Mode()&os.ModeSetuid != 0 {
		mode |= 0o4000
	}
	if info.Mode()&os.ModeSetgid != 0 {
		mode |= 0o2000
	}
	if info.Mode()&os.ModeSticky != 0 {
		mode |= 0o1000
	}
	return mode
}

func chmodUnix(path string, mode uint32) error {
	goMode := os.FileMode(mode & 0o777)
	if mode&0o4000 != 0 {
		goMode |= os.ModeSetuid
	}
	if mode&0o2000 != 0 {
		goMode |= os.ModeSetgid
	}
	if mode&0o1000 != 0 {
		goMode |= os.ModeSticky
	}
	return os.Chmod(path, goMode)
}

func sameIdentity(a, b os.FileInfo) bool {
	aStat, aOK := a.Sys().(*syscall.Stat_t)
	bStat, bOK := b.Sys().(*syscall.Stat_t)
	return aOK && bOK && uint64(aStat.Dev) == uint64(bStat.Dev) && aStat.Ino == bStat.Ino
}

func sameFileInfo(a, b os.FileInfo) bool { return sameIdentity(a, b) }

func statIdentity(path string) (fileIdentity, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return fileIdentity{}, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return fileIdentity{}, fmt.Errorf("unsupported filesystem metadata")
	}
	return fileIdentity{device: uint64(stat.Dev), inode: stat.Ino, uid: int(stat.Uid)}, nil
}

func removeOwnedTree(path string, identity fileIdentity, uid int) error {
	current, err := statIdentity(path)
	if err != nil {
		return err
	}
	if current != identity || current.uid != uid {
		return fmt.Errorf("refuse cleanup of staging tree whose ownership changed")
	}
	return os.RemoveAll(path)
}
