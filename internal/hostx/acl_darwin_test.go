//go:build darwin

package hostx

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestOSACLInspectorDetectsACLWhenExtendedAttributesHideMarker(t *testing.T) {
	path := filepath.Join(t.TempDir(), "private")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	inspector := OSACLInspector{}
	if hasACL, err := inspector.HasExtendedACL(path); err != nil || hasACL {
		t.Fatalf("fresh private file: acl=%v err=%v", hasACL, err)
	}
	cmd := exec.Command("/bin/chmod", "+a", "user:"+os.Getenv("USER")+" allow read", path)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("add test ACL: %v: %s", err, output)
	}
	if hasACL, err := inspector.HasExtendedACL(path); err != nil || !hasACL {
		t.Fatalf("ACL on private file admitted: acl=%v err=%v", hasACL, err)
	}
}
