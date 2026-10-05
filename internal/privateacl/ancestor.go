package privateacl

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

type AncestorInspector interface{ HasUnsafeAncestorACL(string) (bool, error) }

// CheckSafeAncestor permits only no ACL or the exact macOS everyone deny-delete
// entry on a traversed ancestor. Private files and immediate private anchors
// continue to use Check, which permits no extended ACL.
func CheckSafeAncestor(path string, expected os.FileInfo, inspector AncestorInspector) error {
	if inspector == nil {
		return fmt.Errorf("ancestor ACL inspector is required")
	}
	return checkACL(path, expected, inspector.HasUnsafeAncestorACL)
}

func unsafeAncestorACL(output string) (bool, error) {
	header, entries, found := strings.Cut(output, "\n")
	fields := strings.Fields(header)
	if !found || len(fields) == 0 || !strings.HasPrefix(fields[0], "d") {
		return true, fmt.Errorf("malformed ancestor ACL inspection")
	}
	entries = strings.TrimSpace(entries)
	if entries == "" {
		return strings.HasSuffix(fields[0], "+"), nil
	}
	return entries != "0: group:everyone deny delete", nil
}

// CheckAncestorChain prevents path substitution through writable, symlinked,
// foreign-owned or ACL-writable directories before first-run writes or runtime
// helper use. The selected private leaf is admitted separately by its caller.
func CheckAncestorChain(path string) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return fmt.Errorf("ancestor path must be clean and absolute")
	}
	for current := path; current != "/"; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err != nil {
			return err
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0022 != 0 || (int(stat.Uid) != os.Getuid() && stat.Uid != 0) {
			return fmt.Errorf("unsafe directory ancestor %s", current)
		}
		if err := CheckSafeAncestor(current, info, OSInspector{}); err != nil {
			return fmt.Errorf("ancestor %s: %w", current, err)
		}
	}
	return nil
}
