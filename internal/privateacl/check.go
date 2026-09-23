// Package privateacl checks the ACL state of private host paths. The path is
// identity-bracketed around pathname ACL inspection because macOS exposes the
// existing host inspector by path, while callers hold an opened file or root.
package privateacl

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

type Inspector interface {
	HasExtendedACL(path string) (bool, error)
}

// Check fails closed when ACL state cannot be established for exactly the
// opened filesystem object described by expected. The caller must also check
// type, owner, mode, link count, and its own os.Root/file identity as needed.
func Check(path string, expected os.FileInfo, inspector Inspector) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || expected == nil || inspector == nil {
		return fmt.Errorf("ACL admission requires canonical absolute path, exact file identity, and inspector")
	}
	before, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect path before ACL check: %w", err)
	}
	if before.Mode()&os.ModeSymlink != 0 || !os.SameFile(before, expected) || !sameSecurityMetadata(before, expected) {
		return fmt.Errorf("path identity changed before ACL check")
	}
	hasACL, inspectErr := inspector.HasExtendedACL(path)
	after, statErr := os.Lstat(path)
	if statErr != nil {
		return fmt.Errorf("inspect path after ACL check: %w", statErr)
	}
	if after.Mode()&os.ModeSymlink != 0 || !os.SameFile(before, after) || !os.SameFile(expected, after) || !sameSecurityMetadata(before, after) {
		return fmt.Errorf("path identity changed during ACL check")
	}
	if inspectErr != nil {
		return fmt.Errorf("ACL inspection failed: %w", inspectErr)
	}
	if hasACL {
		return fmt.Errorf("extended ACL on private path")
	}
	return nil
}

func sameSecurityMetadata(a, b os.FileInfo) bool {
	if a.Mode() != b.Mode() {
		return false
	}
	aStat, aOK := a.Sys().(*syscall.Stat_t)
	bStat, bOK := b.Sys().(*syscall.Stat_t)
	return aOK && bOK && aStat.Uid == bStat.Uid && aStat.Gid == bStat.Gid && aStat.Nlink == bStat.Nlink
}
