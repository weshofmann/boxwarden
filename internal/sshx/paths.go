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

type privateACLInspector interface {
	HasExtendedACL(string) (bool, error)
}

func caDirectory(root string) string  { return filepath.Join(root, "identity", "ssh-user-ca") }
func pinDirectory(root string) string { return filepath.Join(root, "identity", "ssh-host-pins") }

func requirePrivateDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	return requirePrivateDirectoryInfo(path, info)
}

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
	return requirePrivateDirectoryInfo(path, info)
}

func requirePrivateDirectoryInfo(path string, info os.FileInfo) error {
	return requirePrivateDirectoryInfoWithACL(path, info, osPrivateACLInspector{})
}

func requirePrivateDirectoryInfoWithACL(path string, info os.FileInfo, acl privateACLInspector) error {
	if err := validatePrivateDirectoryInfo(info); err != nil {
		return err
	}
	return verifyNoExtendedACL(path, info, acl, validatePrivateDirectoryInfo)
}

func validatePrivateDirectoryInfo(info os.FileInfo) error {
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
	return requirePrivateTreeWithACL(root, path, osPrivateACLInspector{})
}

func requirePrivateTreeWithACL(root, path string, acl privateACLInspector) error {
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
	if err := requirePrivateDirectoryInfoWithACL(root, info, acl); err != nil {
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
		if err := requirePrivateDirectoryInfoWithACL(current, info, acl); err != nil {
			return err
		}
	}
	return nil
}

func requireRuntimeFile(runtimeDirectory, path string, mode os.FileMode) (os.FileInfo, error) {
	if !filepathIsCanonicalAbsolute(runtimeDirectory) || !filepathIsCanonicalAbsolute(path) {
		return nil, fmt.Errorf("runtime path must be canonical and absolute")
	}
	if err := requirePrivateTree(runtimeDirectory, filepath.Dir(path)); err != nil {
		return nil, err
	}
	return requireFileMode(path, mode)
}

func readRuntimeFile(runtimeDirectory, path string, mode os.FileMode) ([]byte, error) {
	if _, err := requireRuntimeFile(runtimeDirectory, path, mode); err != nil {
		return nil, err
	}
	return readFileMode(path, mode)
}

func removeExactFile(path string, expected os.FileInfo, mode os.FileMode) error {
	if expected == nil {
		return fmt.Errorf("expected file is required")
	}
	root, err := openVerifiedRoot(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer root.Close()
	current, err := root.Lstat(filepath.Base(path))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil || !os.SameFile(expected, current) {
		return fmt.Errorf("temporary file changed before cleanup")
	}
	if err := requireFileInfo(path, current, mode); err != nil {
		return err
	}
	return root.Remove(filepath.Base(path))
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
	if err := requireFileInfo(path, info, mode); err != nil {
		return nil, err
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
	if err := requireFileInfo(path, before, mode); err != nil {
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
	if err := requireFileInfo(path, after, mode); err != nil {
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
	if err := requireFileInfo(path, final, mode); err != nil {
		return nil, err
	}
	return contents, nil
}

func requireFileInfo(path string, info os.FileInfo, mode os.FileMode) error {
	return requireFileInfoWithACL(path, info, mode, osPrivateACLInspector{})
}

func requireFileInfoWithACL(path string, info os.FileInfo, mode os.FileMode, acl privateACLInspector) error {
	validate := func(candidate os.FileInfo) error { return validateFileInfo(candidate, mode) }
	if err := validate(info); err != nil {
		return err
	}
	return verifyNoExtendedACL(path, info, acl, validate)
}

func validateFileInfo(info os.FileInfo, mode os.FileMode) error {
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

// verifyNoExtendedACL brackets the pathname-only Darwin ACL query with two
// lstat checks. File reads and writes separately bind the same inode through
// os.Root/no-follow descriptors; this check prevents a pathname replacement
// during admission without widening the trusted-path surface.
func verifyNoExtendedACL(path string, initial os.FileInfo, acl privateACLInspector, validate func(os.FileInfo) error) error {
	if acl == nil {
		return fmt.Errorf("ACL inspector is required")
	}
	for range 2 {
		extended, err := acl.HasExtendedACL(path)
		if err != nil || extended {
			return fmt.Errorf("path has unverifiable or extended ACL")
		}
		after, err := os.Lstat(path)
		if err != nil || !os.SameFile(initial, after) {
			return fmt.Errorf("path changed while checking ACL")
		}
		if err := validate(after); err != nil {
			return err
		}
		initial = after
	}
	return nil
}

func openVerifiedRoot(path string) (*os.Root, error) {
	before, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if err := requirePrivateDirectoryInfo(path, before); err != nil {
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
	if err := requirePrivateDirectoryInfo(path, after); err != nil {
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
	if err := requireRegularOwnedSingleLink(path, before); err != nil {
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
	if err := requireRegularOwnedSingleLink(path, after); err != nil {
		return err
	}
	if err := file.Chmod(publicFileMode); err != nil {
		return err
	}
	after, err = file.Stat()
	if err != nil {
		return err
	}
	return requireFileInfo(path, after, publicFileMode)
}

func requireRegularOwnedSingleLink(path string, info os.FileInfo) error {
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("must be a regular non-symlink file")
	}
	if links, ok := linkCount(info); !ok || links != 1 {
		return fmt.Errorf("must have exactly one link")
	}
	if owner, ok := fileOwner(info); !ok || owner != currentUID() {
		return fmt.Errorf("file must be owned by the current operator")
	}
	return requireFileInfo(path, info, info.Mode().Perm())
}

func filepathIsCanonicalAbsolute(path string) bool {
	return filepath.IsAbs(path) && filepath.Clean(path) == path
}
