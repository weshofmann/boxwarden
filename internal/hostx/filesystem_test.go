package hostx

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestCanonicalPathRejectsSymlinkTraversalAndEscapingRoot(t *testing.T) {
	root := t.TempDir()
	inside := filepath.Join(root, "inside")
	if err := os.Mkdir(inside, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := CanonicalPath(root, inside); err != nil {
		t.Fatalf("CanonicalPath(inside) error = %v", err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(inside, link); err != nil {
		t.Fatal(err)
	}
	if _, err := CanonicalPath(root, link); err == nil {
		t.Fatal("CanonicalPath(symlink) error = nil, want rejection")
	}
	if _, err := CanonicalPath(root, filepath.Dir(root)); err == nil {
		t.Fatal("CanonicalPath(escape) error = nil, want rejection")
	}
}

func TestOpenVerifiedRegularBindsDigestAndRejectsHardlinks(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "softnet")
	contents := []byte("qualified fixture")
	if err := os.WriteFile(path, contents, 0o755); err != nil {
		t.Fatal(err)
	}
	digest := fmt.Sprintf("%x", sha256.Sum256(contents))
	file, err := openVerifiedRegular(path, digest, true)
	if err != nil {
		t.Fatalf("openVerifiedRegular() error = %v", err)
	}
	file.Close()
	if _, err := openVerifiedRegular(path, fmt.Sprintf("%064d", 0), true); err == nil {
		t.Fatal("openVerifiedRegular(wrong digest) error = nil")
	}
	if err := os.Link(path, filepath.Join(root, "second-link")); err != nil {
		t.Fatal(err)
	}
	if _, err := openVerifiedRegular(path, digest, true); err == nil {
		t.Fatal("openVerifiedRegular(hardlink) error = nil")
	}
}
