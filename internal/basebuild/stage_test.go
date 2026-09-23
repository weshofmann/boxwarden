package basebuild

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestStageBuildInputsRetainsSnapshotAfterSourceChanges(t *testing.T) {
	root := t.TempDir()
	root, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(root, "source")
	attempt := filepath.Join(root, "attempt")
	if err := os.Mkdir(source, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(attempt, 0700); err != nil {
		t.Fatal(err)
	}
	iso := filepath.Join(root, "installer.iso")
	if err := os.WriteFile(iso, []byte("pinned-iso"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, name := range stagedGuestDefinitionFiles {
		path := filepath.Join(source, name)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	staged, err := stageBuildInputs(context.Background(), Inputs{ISOPath: iso, GuestDefinitionRoot: source}, attempt)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(iso, []byte("drift"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, stagedGuestDefinitionFiles[0]), []byte("drift"), 0700); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(staged.ISOPath)
	if err != nil || string(data) != "pinned-iso" {
		t.Fatalf("staged ISO changed: %q %v", data, err)
	}
	data, err = os.ReadFile(filepath.Join(staged.GuestDefinitionRoot, stagedGuestDefinitionFiles[0]))
	if err != nil || string(data) != stagedGuestDefinitionFiles[0] {
		t.Fatalf("staged definition changed: %q %v", data, err)
	}
}
