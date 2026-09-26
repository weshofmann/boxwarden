package recipe

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGuestDefinitionDigestBindsEveryTrackedBuildInput(t *testing.T) {
	root := t.TempDir()
	for _, name := range guestDefinitionFiles {
		full := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(name), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	base, err := GuestDefinitionDigest(root)
	if err != nil || len(base) != 64 {
		t.Fatalf("base digest = %q, %v", base, err)
	}
	for _, name := range guestDefinitionFiles {
		full := filepath.Join(root, name)
		if err := os.WriteFile(full, []byte(name+" changed"), 0o600); err != nil {
			t.Fatal(err)
		}
		got, err := GuestDefinitionDigest(root)
		if err != nil || got == base {
			t.Fatalf("%s did not invalidate definition digest: %q, %v", name, got, err)
		}
		if err := os.WriteFile(full, []byte(name), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	link := filepath.Join(root, guestDefinitionFiles[0])
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, guestDefinitionFiles[1]), link); err != nil {
		t.Fatal(err)
	}
	if _, err := GuestDefinitionDigest(root); err == nil || !strings.Contains(err.Error(), "regular") {
		t.Fatalf("linked build input was accepted: %v", err)
	}
}
