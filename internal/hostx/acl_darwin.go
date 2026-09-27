//go:build darwin

package hostx

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/weshofmann/boxwarden/internal/execx"
)

// OSACLInspector uses the system ls ACL marker with a closed, fixed locale.
// The exact path is passed as one argv element and never interpreted by a shell.
type OSACLInspector struct{}

func (OSACLInspector) HasExtendedACL(path string) (bool, error) {
	if !canonicalAbsolute(path) {
		return false, fmt.Errorf("ACL path must be canonical and absolute")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, err := (execx.OSRunner{MaxOutputBytes: 8 << 10}).Run(ctx, execx.Command{
		Path: "/bin/ls", Args: []string{"-lde", path}, Env: []string{"LC_ALL=C", "LANG=C"},
	})
	if err != nil || result.Truncated {
		return false, fmt.Errorf("inspect ACL for %q: %w", path, err)
	}
	line := result.Stdout
	entries := ""
	if newline := strings.IndexByte(line, '\n'); newline >= 0 {
		entries = line[newline+1:]
		line = line[:newline]
	}
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return false, fmt.Errorf("inspect ACL for %q: malformed ls output", path)
	}
	// On current macOS, an extended-attribute marker (@) can appear on the
	// first line even when an ACL is present. With -e, ls prints the ACL entries
	// on subsequent lines; inspect those as well as the legacy + marker.
	return strings.HasSuffix(fields[0], "+") || strings.TrimSpace(entries) != "", nil
}
