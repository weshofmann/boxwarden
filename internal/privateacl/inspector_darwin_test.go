//go:build darwin

package privateacl

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestOSAncestorAllowsOnlyDefaultDenialAndLeavesPrivateCheckStrict(t *testing.T) {
	path := filepath.Join(t.TempDir(), "protected-ancestor")
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	// The fixture's deny-delete ACL intentionally prevents normal TempDir
	// removal. Restore only this newly created fixture before automatic cleanup.
	t.Cleanup(func() {
		if out, err := exec.Command("/bin/chmod", "-N", path).CombinedOutput(); err != nil {
			t.Errorf("remove synthetic ACL: %v %s", err, out)
		}
	})
	if out, err := exec.Command("/bin/chmod", "+a", "everyone deny delete", path).CombinedOutput(); err != nil {
		t.Fatalf("synthetic ACL: %v %s", err, out)
	}
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := CheckSafeAncestor(path, info, OSInspector{}); err != nil {
		t.Fatal(err)
	}
	if err := Check(path, info, OSInspector{}); err == nil {
		t.Fatal("private leaf accepted default ancestor ACL")
	}
	if out, err := exec.Command("/bin/chmod", "+a", "everyone allow write", path).CombinedOutput(); err != nil {
		t.Fatalf("synthetic extra ACL: %v %s", err, out)
	}
	info, err = os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := CheckSafeAncestor(path, info, OSInspector{}); err == nil {
		t.Fatal("ancestor accepted access grant")
	}
}

func TestOSInspectorAdmitsFreshPrivateFileOnDarwin(t *testing.T) {
	path := filepath.Join(t.TempDir(), "private")
	if err := os.WriteFile(path, []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := Check(path, info, OSInspector{}); err != nil {
		t.Fatalf("fresh private file failed OS ACL inspection: %v", err)
	}
}

func TestAncestorChainRejectsWritableDirectoryAbovePrivateLeaf(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	parent := filepath.Join(root, "shared")
	leaf := filepath.Join(parent, "private")
	if err := os.MkdirAll(leaf, 0700); err != nil {
		t.Fatal(err)
	}
	if err := CheckAncestorChain(leaf); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(parent, 0775); err != nil {
		t.Fatal(err)
	}
	if err := CheckAncestorChain(leaf); err == nil {
		t.Fatal("private leaf beneath writable ancestor admitted")
	}
}
