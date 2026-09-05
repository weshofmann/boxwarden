//go:build darwin

package sshx

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/weshofmann/boxwarden/internal/execx"
)

// osPrivateACLInspector performs the small, argv-only Darwin ACL observation.
// The caller rechecks path metadata around use because ls necessarily accepts a
// path, while reads and writes are protected with no-follow descriptors.
type osPrivateACLInspector struct{}

func (osPrivateACLInspector) HasExtendedACL(path string) (bool, error) {
	if !filepathIsCanonicalAbsolute(path) {
		return false, fmt.Errorf("ACL path must be canonical and absolute")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, err := (execx.OSRunner{MaxOutputBytes: 8 << 10}).Run(ctx, execx.Command{
		Path: "/bin/ls", Args: []string{"-lde", path}, Env: []string{"LC_ALL=C", "LANG=C", "TZ=UTC"},
	})
	if err != nil || result.Truncated {
		return false, fmt.Errorf("inspect ACL for %q: %w", path, err)
	}
	line := result.Stdout
	if newline := strings.IndexByte(line, '\n'); newline >= 0 {
		line = line[:newline]
	}
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return false, fmt.Errorf("inspect ACL for %q: malformed ls output", path)
	}
	return strings.HasSuffix(fields[0], "+"), nil
}
