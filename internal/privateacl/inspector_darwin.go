//go:build darwin

package privateacl

import (
	"context"
	"fmt"
	"github.com/weshofmann/boxwarden/internal/execx"
	"github.com/weshofmann/boxwarden/internal/hostx"
	"time"
)

// OSInspector uses the established bounded macOS ACL inspector.
type OSInspector struct{}

func (OSInspector) HasExtendedACL(path string) (bool, error) {
	return (hostx.OSACLInspector{}).HasExtendedACL(path)
}

func (OSInspector) HasUnsafeAncestorACL(path string) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, err := (execx.OSRunner{MaxOutputBytes: 8 << 10}).Run(ctx, execx.Command{Path: "/bin/ls", Args: []string{"-lde", path}, Env: []string{"LC_ALL=C", "LANG=C"}})
	if err != nil || result.Truncated {
		return true, fmt.Errorf("ancestor ACL inspection unavailable: %v", err)
	}
	return unsafeAncestorACL(result.Stdout)
}
