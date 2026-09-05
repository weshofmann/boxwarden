package hostx

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// CanonicalPath proves that target is lexically beneath root and that every
// component from root through target is a non-symlink. It does not silently
// resolve an attacker-controlled link into a different tree.
func CanonicalPath(root, target string) (string, error) {
	if !canonicalAbsolute(root) || !canonicalAbsolute(target) {
		return "", fmt.Errorf("root and target must be canonical absolute paths")
	}
	relative, err := filepath.Rel(root, target)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("target is outside root")
	}
	for current := root; ; {
		info, err := os.Lstat(current)
		if err != nil {
			return "", fmt.Errorf("inspect %q: %w", current, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("symlink component %q is prohibited", current)
		}
		if current == target {
			return target, nil
		}
		next := strings.TrimPrefix(target, current+string(filepath.Separator))
		component := strings.SplitN(next, string(filepath.Separator), 2)[0]
		current = filepath.Join(current, component)
	}
}

func links(info os.FileInfo) uint64 {
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		return uint64(stat.Nlink)
	}
	return 0
}

func SHA256File(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}
