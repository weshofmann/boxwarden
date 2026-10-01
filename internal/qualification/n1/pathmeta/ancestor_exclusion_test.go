//go:build (!n1diagnostic && !n1clipboarddiagnostic) || n1candidate

package pathmeta

import "testing"

func TestOrdinaryOrCandidateReaderHasNoExactAncestorCapability(t *testing.T) {
	if _, ok := any(OSInspector{}).(interface{ HasExactDenyDeleteACL(string) (bool, error) }); ok {
		t.Fatal("ordinary or candidate reader acquired exact ACL exception")
	}
}
