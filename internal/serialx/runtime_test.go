package serialx

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func privateGeneration(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), requestGeneration)
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestSerialSubtreeRejectsAdoptionBeforeAllocation(t *testing.T) {
	for _, kind := range []string{"directory", "file", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			generation := privateGeneration(t)
			path := filepath.Join(generation, "serial")
			var err error
			switch kind {
			case "directory":
				err = os.Mkdir(path, 0o700)
			case "file":
				err = os.WriteFile(path, []byte("foreign"), 0o600)
			case "symlink":
				err = os.Symlink(t.TempDir(), path)
			}
			if err != nil {
				t.Fatal(err)
			}
			allocated := false
			if _, err := createRuntime(context.Background(), generation, func() (*os.File, *os.File, error) {
				allocated = true
				return nil, nil, errors.New("unexpected allocation")
			}); err == nil {
				t.Fatal("adopted pre-existing serial entry")
			}
			if allocated {
				t.Fatal("allocated PTY before rejecting foreign subtree")
			}
			if _, err := os.Lstat(path); err != nil {
				t.Fatalf("removed foreign subtree: %v", err)
			}
		})
	}
}

func TestSerialRejectsUnsafeGenerationAndCleansFailedAllocation(t *testing.T) {
	generation := privateGeneration(t)
	link := filepath.Join(t.TempDir(), requestGeneration)
	if err := os.Symlink(generation, link); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"", "/", "relative", generation + "/../" + requestGeneration, link} {
		if _, err := CreateRuntime(context.Background(), path); err == nil {
			t.Fatalf("accepted unsafe generation %q", path)
		}
	}
	if err := os.Chmod(generation, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := CreateRuntime(context.Background(), generation); err == nil {
		t.Fatal("accepted public generation")
	}
	if err := os.Chmod(generation, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := createRuntime(context.Background(), generation, func() (*os.File, *os.File, error) { return nil, nil, errors.New("allocation failed") }); err == nil {
		t.Fatal("allocation failure lost")
	}
	if _, err := os.Lstat(filepath.Join(generation, "serial")); !os.IsNotExist(err) {
		t.Fatalf("failed allocation retained serial: %v", err)
	}
	if _, err := os.Stat(generation); err != nil {
		t.Fatal("removed generation")
	}
}
