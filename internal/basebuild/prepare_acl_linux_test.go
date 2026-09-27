//go:build linux

package basebuild

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/weshofmann/boxwarden/internal/privateacl"
)

func TestProductionPrivateStateRootFailsClosedWithoutMacACLInspector(t *testing.T) {
	previous := basebuildACLInspector
	basebuildACLInspector = privateacl.OSInspector{}
	t.Cleanup(func() { basebuildACLInspector = previous })
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := privateStateRoot(dir); err == nil {
		t.Fatal("Linux admitted M1A private host state without supported ACL inspection")
	}
}
