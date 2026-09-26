//go:build linux

package privateacl

import "fmt"

// Linux builds support deterministic tests but cannot admit M1A host state.
type OSInspector struct{}

func (OSInspector) HasExtendedACL(string) (bool, error) {
	return false, fmt.Errorf("private ACL inspection is unsupported on Linux")
}
