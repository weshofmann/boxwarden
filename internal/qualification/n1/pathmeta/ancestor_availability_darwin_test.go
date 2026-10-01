//go:build darwin && (n1diagnostic || n1clipboarddiagnostic) && !n1candidate

package pathmeta

import "testing"

// Removing the diagnostic reader's exact ACL capability makes this fail without
// any native ACL command, process query or host filesystem inspection.
func TestDiagnosticReaderHasExactAncestorCapability(t *testing.T) {
	if _, ok := any(OSInspector{}).(interface{ HasExactDenyDeleteACL(string) (bool, error) }); !ok {
		t.Fatal("diagnostic reader lacks exact ancestor ACL capability")
	}
}
