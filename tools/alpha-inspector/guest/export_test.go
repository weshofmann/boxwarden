package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/weshofmann/boxwarden/internal/exportx"
)

func TestSelectedExportStreamRoundTripsThroughHostReceiver(t *testing.T) {
	source := t.TempDir()
	if err := os.Mkdir(filepath.Join(source, "project"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "project", "report.txt"), []byte("alpha report"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "project", "notes.txt"), []byte("notes"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "unselected.txt"), []byte("do not return"), 0o600); err != nil {
		t.Fatal(err)
	}
	transaction := [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	var stream bytes.Buffer
	if err := writeSelectedExport(context.Background(), &stream, source, transaction,
		[]string{"project/report.txt", "project"}, defaultGuestExportLimits); err != nil {
		t.Fatal(err)
	}
	parent := t.TempDir()
	if err := os.Chmod(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	output, err := exportx.Receive(context.Background(), io.NopCloser(bytes.NewReader(stream.Bytes())), exportx.Options{
		Parent: parent, TransactionID: transaction, MaxChunkBytes: 1 << 20,
		MaxFileBytes: 256 << 20, MaxTotalBytes: 256 << 20, MaxFiles: 4096,
		MaxDirectories: 4096, MinFreeBytes: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	for name, expected := range map[string]string{"report.txt": "alpha report", "notes.txt": "notes"} {
		body, err := os.ReadFile(filepath.Join(output, "project", name))
		if err != nil || string(body) != expected {
			t.Fatalf("exported %s = %q, %v", name, body, err)
		}
	}
	if _, err := os.Stat(filepath.Join(output, "unselected.txt")); !os.IsNotExist(err) {
		t.Fatalf("unselected file was returned: %v", err)
	}
}

func TestSelectedExportRejectsLinksUnsafeNamesAndLimits(t *testing.T) {
	for _, scenario := range []struct {
		name  string
		setup func(t *testing.T, root string)
		path  string
		limit guestExportLimits
	}{
		{"symlink", func(t *testing.T, root string) {
			if err := os.Symlink("target.txt", filepath.Join(root, "link.txt")); err != nil {
				t.Fatal(err)
			}
		}, "link.txt", defaultGuestExportLimits},
		{"unsafe name", func(t *testing.T, root string) {
			if err := os.WriteFile(filepath.Join(root, "bad name.txt"), []byte("x"), 0o600); err != nil {
				t.Fatal(err)
			}
		}, "bad name.txt", defaultGuestExportLimits},
		{"byte limit", func(t *testing.T, root string) {
			if err := os.WriteFile(filepath.Join(root, "large.txt"), []byte("large"), 0o600); err != nil {
				t.Fatal(err)
			}
		}, "large.txt", guestExportLimits{maxFile: 4, maxTotal: 4, maxFiles: 4, maxDirectories: 4}},
		{"linked descendant", func(t *testing.T, root string) {
			if err := os.Mkdir(filepath.Join(root, "tree"), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink("elsewhere", filepath.Join(root, "tree", "link")); err != nil {
				t.Fatal(err)
			}
		}, "tree", defaultGuestExportLimits},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			root := t.TempDir()
			scenario.setup(t, root)
			var stream bytes.Buffer
			if err := writeSelectedExport(context.Background(), &stream, root, [16]byte{1}, []string{scenario.path}, scenario.limit); err == nil {
				t.Fatal("unsafe selection produced a complete export")
			}
		})
	}
}
