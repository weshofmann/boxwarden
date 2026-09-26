//go:build linux

package privateacl

import "testing"

func TestOSInspectorFailsClosedOnLinux(t *testing.T) {
	if acl, err := (OSInspector{}).HasExtendedACL("/tmp/workspace.raw"); err == nil || acl {
		t.Fatalf("unsupported Linux ACL inspector admitted path: acl=%v err=%v", acl, err)
	}
}
