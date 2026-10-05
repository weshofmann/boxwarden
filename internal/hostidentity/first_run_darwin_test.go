//go:build darwin && cgo

package hostidentity

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFirstRunLocationRefusesSameFilesystemWithoutWrites(t *testing.T) {
	root, _ := filepath.EvalSymlinks(t.TempDir())
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	_, err = ObserveFirstRunLocation(root, root)
	if err == nil || !strings.Contains(err.Error(), "different") {
		t.Fatalf("same filesystem admission = %v", err)
	}
	after, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(before) != len(after) {
		t.Fatal("observation wrote entries")
	}
}

func TestFirstRunLocationRefusesUnsafeSelection(t *testing.T) {
	root, _ := filepath.EvalSymlinks(t.TempDir())
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	data := filepath.Join(root, "data")
	if err := os.Mkdir(data, 0775); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(data, 0775); err != nil {
		t.Fatal(err)
	}
	if _, err := ObserveFirstRunLocation(data, root); err == nil || !strings.Contains(err.Error(), "0700") {
		t.Fatalf("unsafe mode = %v", err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(root, link); err != nil {
		t.Fatal(err)
	}
	if _, err := ObserveFirstRunLocation(link, root); err == nil {
		t.Fatal("accepted symlink")
	}
	if _, err := ObserveFirstRunLocation(root+"/../"+filepath.Base(root), root); err == nil {
		t.Fatal("accepted unclean path")
	}
}
