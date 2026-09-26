package importx

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func publishedImportFixture(t *testing.T) (string, string) {
	t.Helper()
	staging, published := privateDirectory(t), privateDirectory(t)
	captured := filepath.Join(staging, testTransaction)
	if err := os.Mkdir(captured, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, root := range []string{captured, published} {
		for _, name := range []string{"nested", "empty"} {
			if err := os.Mkdir(filepath.Join(root, name), 0o700); err != nil {
				t.Fatal(err)
			}
		}
		for name, content := range map[string]string{"README.txt": "synthetic\n", "nested/data.json": "{}\n"} {
			if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	entries := []Entry{{Path: "empty", Kind: "directory"}, {Path: "nested", Kind: "directory"}}
	for name, content := range map[string]string{"README.txt": "synthetic\n", "nested/data.json": "{}\n"} {
		digest := sha256.Sum256([]byte(content))
		entries = append(entries, Entry{Path: name, Kind: "file", Size: int64(len(content)), SHA256: hex.EncodeToString(digest[:])})
	}
	raw, err := json.Marshal(struct {
		Version int     `json:"version"`
		Entries []Entry `json:"entries"`
	}{Version: 1, Entries: entries})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(captured, manifestName), append(raw, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := InspectSnapshot(staging, testTransaction); err != nil {
		t.Fatal(err)
	}
	return staging, published
}

func TestComparePublishedTreeRequiresExactCapturedBytesAndMembership(t *testing.T) {
	staging, published := publishedImportFixture(t)
	got, err := ComparePublishedTree(staging, testTransaction, published)
	if err != nil || got.FileCount != 2 || got.DirectoryCount != 2 {
		t.Fatalf("exact published tree rejected: %+v, %v", got, err)
	}
	for _, test := range []struct {
		name   string
		change func(*testing.T, string)
	}{
		{"changed bytes", func(t *testing.T, root string) {
			if err := os.WriteFile(filepath.Join(root, "nested/data.json"), []byte("[]\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{"extra file", func(t *testing.T, root string) {
			if err := os.WriteFile(filepath.Join(root, "extra.txt"), []byte("extra"), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{"missing empty directory", func(t *testing.T, root string) {
			if err := os.Remove(filepath.Join(root, "empty")); err != nil {
				t.Fatal(err)
			}
		}},
		{"symlink", func(t *testing.T, root string) {
			if err := os.Remove(filepath.Join(root, "README.txt")); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink("nested/data.json", filepath.Join(root, "README.txt")); err != nil {
				t.Fatal(err)
			}
		}},
		{"hardlink", func(t *testing.T, root string) {
			if err := os.Link(filepath.Join(root, "README.txt"), filepath.Join(t.TempDir(), "second-link")); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			staging, published := publishedImportFixture(t)
			test.change(t, published)
			if _, err := ComparePublishedTree(staging, testTransaction, published); err == nil {
				t.Fatal("changed published tree accepted as retained import")
			}
		})
	}
}
