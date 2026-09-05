//go:build linux

package hostx

import "fmt"

// Linux is compile-supported for deterministic CI only; the M1A host policy is
// Darwin-specific, so ACL admission is deliberately unavailable here.
type OSACLInspector struct{}

func (OSACLInspector) HasExtendedACL(string) (bool, error) {
	return false, fmt.Errorf("ACL inspection is unsupported on unqualified Linux hosts")
}
