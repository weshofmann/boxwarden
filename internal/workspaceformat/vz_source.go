package workspaceformat

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/weshofmann/boxwarden/internal/execx"
)

// cleanSourceCommit binds a private formatter bundle to the exact checked-out
// source used for this operation. It does not claim cryptographic provenance
// against another process with the trusted operator's filesystem authority.
func cleanSourceCommit(ctx context.Context, root string) (string, error) {
	if !filepath.IsAbs(root) || filepath.Clean(root) != root {
		return "", fmt.Errorf("formatter source root is not clean and absolute")
	}
	runner := execx.OSRunner{MaxOutputBytes: 8 << 10, MaxStdinBytes: 0}
	run := func(args ...string) (string, error) {
		result, err := runner.Run(ctx, execx.Command{Path: "/usr/bin/git", Args: append([]string{"-C", root}, args...),
			Env: []string{"PATH=/usr/bin:/bin", "LANG=C", "LC_ALL=C"}})
		if err != nil || result.Truncated {
			return "", fmt.Errorf("formatter source git check failed: %v", err)
		}
		return strings.TrimSpace(result.Stdout), nil
	}
	top, err := run("rev-parse", "--show-toplevel")
	canonicalRoot, rootErr := filepath.EvalSymlinks(root)
	canonicalTop, topErr := filepath.EvalSymlinks(top)
	if err != nil || rootErr != nil || topErr != nil || canonicalTop != canonicalRoot {
		return "", fmt.Errorf("formatter source root differs from Git top level: %v", err)
	}
	commit, err := run("rev-parse", "HEAD")
	if err != nil || !lowerHex(commit, 40) {
		return "", fmt.Errorf("formatter source revision is invalid: %v", err)
	}
	status, err := run("status", "--porcelain", "--untracked-files=all")
	if err != nil || status != "" {
		return "", fmt.Errorf("formatter source checkout is not clean: %v", err)
	}
	return commit, nil
}
