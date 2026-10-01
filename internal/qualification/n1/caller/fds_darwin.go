//go:build darwin

package caller

import (
	"os"
	"path/filepath"
	"sort"
	"syscall"

	"github.com/weshofmann/boxwarden/internal/qualification/n1/fixed"
)

const maxInheritedDescriptors = 65536
const inheritedDescriptorDirectory = "/dev/fd"

// This is a bounded observation of the trusted Darwin devfs descriptor surface,
// not a process census. The caller trusts that no cooperating code creates
// arbitrary non-CLOEXEC descriptors concurrently with the final observation and
// exec. Directory/descriptor brackets do not claim atomic hostile-host proof.
func inheritedDescriptors() (result []uintptr, err error) {
	parents := []string{"/", "/dev", inheritedDescriptorDirectory}
	before := make([]os.FileInfo, len(parents))
	for i, p := range parents {
		f, e := os.Lstat(p)
		if e != nil || !protectedDescriptorDirectory(f) {
			return nil, fixed.ErrRefused
		}
		before[i] = f
	}
	canonical, e := filepath.EvalSymlinks(inheritedDescriptorDirectory)
	if e != nil || canonical != inheritedDescriptorDirectory {
		return nil, fixed.ErrRefused
	}
	fd, e := syscall.Open(inheritedDescriptorDirectory, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if e != nil {
		return nil, fixed.ErrRefused
	}
	directory := os.NewFile(uintptr(fd), inheritedDescriptorDirectory)
	if directory == nil {
		if syscall.Close(fd) != nil {
			return nil, fixed.ErrRefused
		}
		return nil, fixed.ErrRefused
	}
	defer func() {
		if directory.Close() != nil {
			result = nil
			err = fixed.ErrRefused
		}
	}()
	opened, e := directory.Stat()
	if e != nil || !sameDescriptorDirectory(before[2], opened) {
		return nil, fixed.ErrRefused
	}
	var fs syscall.Statfs_t
	if syscall.Fstatfs(fd, &fs) != nil || nativeDescriptorString(fs.Fstypename[:]) != "devfs" || nativeDescriptorString(fs.Mntonname[:]) != "/dev" || nativeDescriptorString(fs.Mntfromname[:]) != "devfs" {
		return nil, fixed.ErrRefused
	}
	// The native reader explicitly owns and checks its fdopendir duplicate and
	// closedir. Go's directory reader hides that duplicate and its close result.
	names, duplicate, e := nativeDescriptorNames(fd)
	if e != nil {
		return nil, fixed.ErrRefused
	}
	result, seen, e := admitDescriptorNames(names, uintptr(fd), duplicate)
	if e != nil {
		return nil, fixed.ErrRefused
	}
	for fd := uintptr(0); fd <= 3; fd++ {
		if !seen[fd] || fd == directory.Fd() || fd == duplicate {
			return nil, fixed.ErrRefused
		}
	}
	for i, p := range parents {
		after, e := os.Lstat(p)
		if e != nil || !sameDescriptorDirectory(before[i], after) {
			return nil, fixed.ErrRefused
		}
	}
	final, e := directory.Stat()
	canonical, e2 := filepath.EvalSymlinks(inheritedDescriptorDirectory)
	if e != nil || e2 != nil || canonical != inheritedDescriptorDirectory || !sameDescriptorDirectory(opened, final) {
		return nil, fixed.ErrRefused
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result, nil
}
func nativeDescriptorString(v []int8) string {
	b := make([]byte, 0, len(v))
	for _, c := range v {
		if c == 0 {
			return string(b)
		}
		b = append(b, byte(c))
	}
	return ""
}
func protectedDescriptorDirectory(f os.FileInfo) bool {
	if f == nil || !f.IsDir() || f.Mode()&(os.ModeSymlink|os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 || f.Mode().Perm()&0022 != 0 {
		return false
	}
	s, ok := f.Sys().(*syscall.Stat_t)
	return ok && s.Uid == 0 && s.Gid == 0
}
func sameDescriptorDirectory(a, b os.FileInfo) bool {
	return protectedDescriptorDirectory(a) && protectedDescriptorDirectory(b) && os.SameFile(a, b) && a.Mode() == b.Mode()
}
func containInheritedDescriptors() error {
	before, e := inheritedDescriptors()
	if e != nil {
		return fixed.ErrRefused
	}
	for _, fd := range before {
		flags, _, code := syscall.Syscall(syscall.SYS_FCNTL, fd, syscall.F_GETFD, 0)
		if code != 0 {
			return fixed.ErrRefused
		}
		if fd <= 3 {
			if flags&syscall.FD_CLOEXEC != 0 {
				return fixed.ErrRefused
			}
			continue
		}
		if _, _, code = syscall.Syscall(syscall.SYS_FCNTL, fd, syscall.F_SETFD, flags|syscall.FD_CLOEXEC); code != 0 {
			return fixed.ErrRefused
		}
	}
	after, e := inheritedDescriptors()
	if e != nil || len(after) != len(before) {
		return fixed.ErrRefused
	}
	for i, fd := range after {
		if fd != before[i] {
			return fixed.ErrRefused
		}
		flags, _, code := syscall.Syscall(syscall.SYS_FCNTL, fd, syscall.F_GETFD, 0)
		if code != 0 || fd <= 3 && flags&syscall.FD_CLOEXEC != 0 || fd >= 4 && flags&syscall.FD_CLOEXEC == 0 {
			return fixed.ErrRefused
		}
	}
	return nil
}

func admitDescriptorNames(names []uintptr, base, duplicate uintptr) ([]uintptr, map[uintptr]bool, error) {
	if len(names) > maxInheritedDescriptors || base < 4 || duplicate < 4 || base == duplicate {
		return nil, nil, fixed.ErrRefused
	}
	seen := make(map[uintptr]bool, len(names))
	result := make([]uintptr, 0, len(names))
	for _, fd := range names {
		if fd > 2147483647 || seen[fd] {
			return nil, nil, fixed.ErrRefused
		}
		seen[fd] = true
		if fd != base && fd != duplicate {
			result = append(result, fd)
		}
	}
	if !seen[base] || !seen[duplicate] {
		return nil, nil, fixed.ErrRefused
	}
	return result, seen, nil
}
