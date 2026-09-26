//go:build darwin

package sshx

import (
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"testing"
)

func TestOSPrivateACLInspectorDetectsEntryBeneathAttributeMarker(t *testing.T) {
	current, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "credential")
	if err := os.WriteFile(path, []byte("synthetic"), 0o600); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("/bin/chmod", "+a", "user:"+current.Username+" allow read", path)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("add test ACL: %v: %s", err, output)
	}
	if extended, err := (osPrivateACLInspector{}).HasExtendedACL(path); err != nil || !extended {
		t.Fatalf("ACL-bearing credential admitted: extended=%v err=%v", extended, err)
	}
}
