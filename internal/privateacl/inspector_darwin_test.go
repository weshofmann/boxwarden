//go:build darwin

package privateacl

import (
	"os"
	"path/filepath"
	"testing"
)

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
