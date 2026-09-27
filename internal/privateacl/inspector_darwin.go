//go:build darwin

package privateacl

import "github.com/weshofmann/boxwarden/internal/hostx"

// OSInspector uses the established bounded macOS ACL inspector.
type OSInspector struct{}

func (OSInspector) HasExtendedACL(path string) (bool, error) {
	return (hostx.OSACLInspector{}).HasExtendedACL(path)
}
