package importx

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/weshofmann/boxwarden/internal/renamex"
)

const testTransaction = "11111111-2222-4333-8444-555555555555"

func privateDirectory(t *testing.T) string {
	t.Helper()
	path, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestCaptureSourceCreatesBoundedPrivateSnapshot(t *testing.T) {
	source, parent := privateDirectory(t), privateDirectory(t)
	if err := os.Mkdir(filepath.Join(source, "nested"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "README.md"), []byte("alpha source\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "nested", "main.go"), []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := CaptureSource(context.Background(), source, parent, testTransaction)
	if err != nil {
		t.Fatal(err)
	}
	if got.TransactionID != testTransaction || got.FileCount != 2 || got.DirectoryCount != 1 || got.TotalBytes != int64(len("alpha source\n")+len("package main\n")) || len(got.Digest) != 64 {
		t.Fatalf("snapshot metadata = %#v", got)
	}
	if got.Directory != filepath.Join(parent, testTransaction) {
		t.Fatalf("snapshot directory = %q", got.Directory)
	}
	if raw, err := os.ReadFile(filepath.Join(got.Directory, "nested", "main.go")); err != nil || string(raw) != "package main\n" {
		t.Fatalf("captured source = %q, %v", raw, err)
	}
	if info, err := os.Stat(filepath.Join(got.Directory, "README.md")); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("snapshot file mode = %v, %v", info, err)
	}
	if _, err := CaptureSource(context.Background(), source, parent, testTransaction); err == nil {
		t.Fatal("existing snapshot was overwritten")
	}
	if raw, err := os.ReadFile(filepath.Join(source, "README.md")); err != nil || string(raw) != "alpha source\n" {
		t.Fatalf("source changed: %q, %v", raw, err)
	}
}

func TestCaptureSourceRejectsLinksUnsafeNamesAndSize(t *testing.T) {
	for _, test := range []struct {
		name  string
		setup func(*testing.T, string)
	}{
		{"symlink", func(t *testing.T, source string) {
			if err := os.Symlink("/etc/passwd", filepath.Join(source, "link")); err != nil {
				t.Fatal(err)
			}
		}},
		{"hardlink", func(t *testing.T, source string) {
			if err := os.WriteFile(filepath.Join(source, "a"), []byte("x"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Link(filepath.Join(source, "a"), filepath.Join(source, "b")); err != nil {
				t.Fatal(err)
			}
		}},
		{"unsafe name", func(t *testing.T, source string) {
			if err := os.WriteFile(filepath.Join(source, "with space"), []byte("x"), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{"oversized file", func(t *testing.T, source string) {
			file, err := os.Create(filepath.Join(source, "large"))
			if err != nil {
				t.Fatal(err)
			}
			if err := file.Truncate(4<<20 + 1); err != nil {
				t.Fatal(err)
			}
			if err := file.Close(); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			source, parent := privateDirectory(t), privateDirectory(t)
			test.setup(t, source)
			if _, err := CaptureSource(context.Background(), source, parent, testTransaction); err == nil {
				t.Fatal("unsafe source was captured")
			}
			if _, err := os.Lstat(filepath.Join(parent, testTransaction)); !os.IsNotExist(err) {
				t.Fatalf("failed capture published snapshot: %v", err)
			}
		})
	}
}

func TestCaptureSourceRejectsOverlappingTrees(t *testing.T) {
	parent := privateDirectory(t)
	source := filepath.Join(parent, "source")
	if err := os.Mkdir(source, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "data"), []byte("safe"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := CaptureSource(context.Background(), source, parent, testTransaction); err == nil {
		t.Fatal("source inside staging parent was admitted")
	}
	if _, err := CaptureSource(context.Background(), parent, source, testTransaction); err == nil {
		t.Fatal("staging parent inside source was admitted")
	}
}

func TestCaptureSourceRejectsCancelledAndCleansPartialCapture(t *testing.T) {
	source, parent := privateDirectory(t), privateDirectory(t)
	if err := os.WriteFile(filepath.Join(source, "good"), []byte("safe"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("good", filepath.Join(source, "bad")); err != nil {
		t.Fatal(err)
	}
	if _, err := CaptureSource(context.Background(), source, parent, testTransaction); err == nil {
		t.Fatal("unsafe source was captured")
	}
	entries, err := os.ReadDir(parent)
	if err != nil || len(entries) != 0 {
		t.Fatalf("partial capture remained: %v, %v", entries, err)
	}
	if err := os.Remove(filepath.Join(source, "bad")); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := CaptureSource(ctx, source, parent, testTransaction); err == nil {
		t.Fatal("cancelled capture published")
	}
	entries, err = os.ReadDir(parent)
	if err != nil || len(entries) != 0 {
		t.Fatalf("cancelled capture left staging state: %v, %v", entries, err)
	}
}

func TestRenameExclusiveCannotReplaceDestination(t *testing.T) {
	parent := privateDirectory(t)
	for _, name := range []string{"source", "destination"} {
		if err := os.Mkdir(filepath.Join(parent, name), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	root, err := os.OpenRoot(parent)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err := renamex.NoReplace(root, "source", "destination"); err == nil {
		t.Fatal("existing empty destination was replaced")
	}
	if _, err := os.Stat(filepath.Join(parent, "source")); err != nil {
		t.Fatalf("source was moved: %v", err)
	}
	if _, err := os.Stat(filepath.Join(parent, "destination")); err != nil {
		t.Fatalf("destination was removed: %v", err)
	}
}

func TestInspectSnapshotReadmitsExactCapturedBytes(t *testing.T) {
	source, parent := privateDirectory(t), privateDirectory(t)
	if err := os.Mkdir(filepath.Join(source, "empty"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "project.txt"), []byte("synthetic project\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	captured, err := CaptureSource(context.Background(), source, parent, testTransaction)
	if err != nil {
		t.Fatal(err)
	}
	readmitted, err := InspectSnapshot(parent, testTransaction)
	if err != nil || readmitted.Digest != captured.Digest || readmitted.TotalBytes != captured.TotalBytes {
		t.Fatalf("snapshot readmission: %#v, %v", readmitted, err)
	}
	if err := os.WriteFile(filepath.Join(captured.Directory, "project.txt"), []byte("different project\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := InspectSnapshot(parent, testTransaction); err == nil {
		t.Fatal("changed snapshot bytes were readmitted")
	}
}

func TestInspectSnapshotRejectsUnlistedAndLinkedEntries(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*testing.T, string)
	}{
		{"extra file", func(t *testing.T, directory string) {
			if err := os.WriteFile(filepath.Join(directory, "extra"), []byte("x"), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{"link", func(t *testing.T, directory string) {
			if err := os.Symlink("project.txt", filepath.Join(directory, "unexpected")); err != nil {
				t.Fatal(err)
			}
		}},
		{"manifest", func(t *testing.T, directory string) {
			if err := os.WriteFile(filepath.Join(directory, manifestName), []byte("{}\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			source, parent := privateDirectory(t), privateDirectory(t)
			if err := os.WriteFile(filepath.Join(source, "project.txt"), []byte("synthetic project\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			captured, err := CaptureSource(context.Background(), source, parent, testTransaction)
			if err != nil {
				t.Fatal(err)
			}
			test.mutate(t, captured.Directory)
			if _, err := InspectSnapshot(parent, testTransaction); err == nil {
				t.Fatal("changed snapshot was readmitted")
			}
		})
	}
}

func TestInspectSnapshotRejectsDeclaredFileDisappearingAfterHash(t *testing.T) {
	source, parent := privateDirectory(t), privateDirectory(t)
	if err := os.WriteFile(filepath.Join(source, "project.txt"), []byte("synthetic\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	captured, err := CaptureSource(context.Background(), source, parent, testTransaction)
	if err != nil {
		t.Fatal(err)
	}
	previous := beforeSnapshotWalk
	defer func() { beforeSnapshotWalk = previous }()
	beforeSnapshotWalk = func() {
		if err := os.Remove(filepath.Join(captured.Directory, "project.txt")); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := InspectSnapshot(parent, testTransaction); err == nil {
		t.Fatal("declared file removed after hashing was readmitted")
	}
}
