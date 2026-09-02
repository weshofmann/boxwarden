// Package sshx owns trusted-host SSH management identity and strict client policy.
package sshx

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const (
	privateDirectoryMode = 0o700
	privateFileMode      = 0o600
	publicFileMode       = 0o644
	maxStateFileBytes    = 64 << 10
)

func caDirectory(root string) string  { return filepath.Join(root, "identity", "ssh-user-ca") }
func pinDirectory(root string) string { return filepath.Join(root, "identity", "ssh-host-pins") }

func ensurePrivateDirectory(path string) error {
	if !filepath.IsAbs(path) {
		return fmt.Errorf("private path must be absolute")
	}
	clean := filepath.Clean(path)
	if clean != path {
		return fmt.Errorf("private path must be canonical")
	}
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		parent := filepath.Dir(path)
		if parent == path {
			return fmt.Errorf("private directory does not exist")
		}
		if err := ensurePrivateDirectory(parent); err != nil {
			return err
		}
		if err := os.Mkdir(path, privateDirectoryMode); err != nil && !os.IsExist(err) {
			return fmt.Errorf("create private directory: %w", err)
		}
		info, err = os.Lstat(path)
	}
	if err != nil {
		return fmt.Errorf("inspect private directory: %w", err)
	}
	return requirePrivateDirectoryInfo(info)
}

func requirePrivateDirectoryInfo(info os.FileInfo) error {
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("must be a non-symlink directory")
	}
	if info.Mode().Perm() != privateDirectoryMode {
		return fmt.Errorf("directory must have mode 0700")
	}
	if owner, ok := fileOwner(info); !ok || owner != currentUID() {
		return fmt.Errorf("directory must be owned by the current operator")
	}
	return nil
}

// requirePrivateTree checks every component below the configured private root;
// Lstat is used deliberately so an intermediate symlink cannot redirect state.
func requirePrivateTree(root, path string) error {
	if !filepath.IsAbs(root) || filepath.Clean(root) != root || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return fmt.Errorf("private path must be canonical and absolute")
	}
	relative, err := filepath.Rel(root, path)
	if err != nil || relative == ".." || len(relative) >= 3 && relative[:3] == ".."+string(filepath.Separator) {
		return fmt.Errorf("private path escapes configured root")
	}
	info, err := os.Lstat(root)
	if err != nil {
		return err
	}
	if err := requirePrivateDirectoryInfo(info); err != nil {
		return err
	}
	current := root
	if relative == "." {
		return nil
	}
	for _, part := range splitPath(relative) {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil {
			return err
		}
		if err := requirePrivateDirectoryInfo(info); err != nil {
			return err
		}
	}
	return nil
}

func splitPath(path string) []string {
	var parts []string
	for path != "." && path != "" {
		part := filepath.Base(path)
		parts = append([]string{part}, parts...)
		path = filepath.Dir(path)
	}
	return parts
}

func requirePrivateFile(path string) (os.FileInfo, error) {
	return requireFileMode(path, privateFileMode)
}

func requirePublicFile(path string) (os.FileInfo, error) {
	return requireFileMode(path, publicFileMode)
}

func requireFileMode(path string, mode os.FileMode) (os.FileInfo, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Mode().Perm() != mode || info.Sys() == nil {
		return nil, fmt.Errorf("must be a regular non-symlink file with mode %04o", mode)
	}
	if links, ok := linkCount(info); !ok || links != 1 {
		return nil, fmt.Errorf("must have exactly one link")
	}
	if owner, ok := fileOwner(info); !ok || owner != currentUID() {
		return nil, fmt.Errorf("file must be owned by the current operator")
	}
	return info, nil
}

func readPrivateFile(path string) ([]byte, error) {
	return readFileMode(path, privateFileMode)
}
func readPublicFile(path string) ([]byte, error) { return readFileMode(path, publicFileMode) }
func readFileMode(path string, mode os.FileMode) ([]byte, error) {
	if _, err := requireFileMode(path, mode); err != nil {
		return nil, err
	}
	parent := filepath.Dir(path)
	root, err := openVerifiedRoot(parent)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	name := filepath.Base(path)
	before, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if err := requireFileInfo(before, mode); err != nil {
		return nil, err
	}
	file, err := openNoFollow(root, name, os.O_RDONLY, 0)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	after, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !os.SameFile(before, after) {
		return nil, fmt.Errorf("file changed while opening")
	}
	if err := requireFileInfo(after, mode); err != nil {
		return nil, err
	}
	contents, err := io.ReadAll(io.LimitReader(file, maxStateFileBytes+1))
	if err != nil {
		return nil, err
	}
	if len(contents) > maxStateFileBytes {
		return nil, fmt.Errorf("state file exceeds %d bytes", maxStateFileBytes)
	}
	final, err := root.Lstat(name)
	if err != nil || !os.SameFile(before, final) {
		return nil, fmt.Errorf("file changed while reading")
	}
	if err := requireFileInfo(final, mode); err != nil {
		return nil, err
	}
	return contents, nil
}

func requireFileInfo(info os.FileInfo, mode os.FileMode) error {
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Mode().Perm() != mode {
		return fmt.Errorf("must be a regular non-symlink file with mode %04o", mode)
	}
	if links, ok := linkCount(info); !ok || links != 1 {
		return fmt.Errorf("must have exactly one link")
	}
	if owner, ok := fileOwner(info); !ok || owner != currentUID() {
		return fmt.Errorf("file must be owned by the current operator")
	}
	return nil
}

func openVerifiedRoot(path string) (*os.Root, error) {
	before, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if err := requirePrivateDirectoryInfo(before); err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		return nil, err
	}
	after, err := root.Stat(".")
	if err != nil || !os.SameFile(before, after) {
		root.Close()
		return nil, fmt.Errorf("directory changed while opening")
	}
	if err := requirePrivateDirectoryInfo(after); err != nil {
		root.Close()
		return nil, err
	}
	return root, nil
}

func normalizePublicFile(path string) error {
	root, err := openVerifiedRoot(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer root.Close()
	name := filepath.Base(path)
	before, err := root.Lstat(name)
	if err != nil {
		return err
	}
	if err := requireRegularOwnedSingleLink(before); err != nil {
		return err
	}
	file, err := openNoFollow(root, name, os.O_RDONLY, 0)
	if err != nil {
		return err
	}
	defer file.Close()
	after, err := file.Stat()
	if err != nil || !os.SameFile(before, after) {
		return fmt.Errorf("public key changed while opening")
	}
	if err := requireRegularOwnedSingleLink(after); err != nil {
		return err
	}
	if err := file.Chmod(publicFileMode); err != nil {
		return err
	}
	after, err = file.Stat()
	if err != nil {
		return err
	}
	return requireFileInfo(after, publicFileMode)
}

func requireRegularOwnedSingleLink(info os.FileInfo) error {
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("must be a regular non-symlink file")
	}
	if links, ok := linkCount(info); !ok || links != 1 {
		return fmt.Errorf("must have exactly one link")
	}
	if owner, ok := fileOwner(info); !ok || owner != currentUID() {
		return fmt.Errorf("file must be owned by the current operator")
	}
	return nil
}
