package importx

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func selectedFixture(t *testing.T) string {
	t.Helper()
	source := privateDirectory(t)
	for _, d := range []string{"src", "node_modules", ".git"} {
		if err := os.Mkdir(filepath.Join(source, d), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for name, body := range map[string]string{"README.md": "host original\n", "src/main.go": "package synthetic\n", "node_modules/generated": "excluded synthetic\n", ".git/HEAD": "synthetic history\n"} {
		if err := os.WriteFile(filepath.Join(source, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// An excluded link is never followed, opened or admitted as selected input.
	if err := os.Symlink(filepath.Join(privateDirectory(t), "unavailable"), filepath.Join(source, ".env")); err != nil {
		t.Fatal(err)
	}
	return source
}

func TestPreviewAndCaptureShareExactLiteralSelection(t *testing.T) {
	source, parent := selectedFixture(t), privateDirectory(t)
	selection := Selection{Excludes: []string{"node_modules", ".git", ".env"}}
	before, err := os.Stat(source)
	if err != nil {
		t.Fatal(err)
	}
	preview, err := PreviewSource(context.Background(), source, selection)
	if err != nil {
		t.Fatal(err)
	}
	if preview.TransactionID != "" || preview.Directory != "" || preview.FileCount != 2 || preview.DirectoryCount != 1 || preview.TotalBytes != int64(len("host original\npackage synthetic\n")) {
		t.Fatalf("preview: %#v", preview)
	}
	var paths []string
	for _, entry := range preview.Entries {
		paths = append(paths, entry.Path)
	}
	if !reflect.DeepEqual(paths, []string{"README.md", "src", "src/main.go"}) {
		t.Fatalf("selection paths: %v", paths)
	}
	after, err := os.Stat(source)
	if err != nil || !sameSourceInfo(before, after) {
		t.Fatalf("preview changed source root: %v", err)
	}
	selection.ExpectedDigest = preview.Digest
	captured, err := CaptureSelectedSource(context.Background(), source, parent, testTransaction, selection)
	if err != nil {
		t.Fatal(err)
	}
	if captured.Digest != preview.Digest || !reflect.DeepEqual(captured.Entries, preview.Entries) {
		t.Fatalf("preview/capture differ: %#v %#v", preview, captured)
	}
	if _, err := os.Lstat(filepath.Join(captured.Directory, "node_modules")); !os.IsNotExist(err) {
		t.Fatalf("excluded directory captured: %v", err)
	}
	if raw, err := os.ReadFile(filepath.Join(source, "README.md")); err != nil || string(raw) != "host original\n" {
		t.Fatalf("host original changed: %q %v", raw, err)
	}
	if info, err := os.Stat(filepath.Join(source, "README.md")); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("host permissions changed: %v %v", info, err)
	}
	inspected, err := InspectSnapshot(parent, testTransaction)
	if err != nil || inspected.Digest != preview.Digest {
		t.Fatalf("selected snapshot readmission: %#v %v", inspected, err)
	}
}

func TestPinnedPreviewRejectsChangedSourceWithoutPublishing(t *testing.T) {
	source, parent := selectedFixture(t), privateDirectory(t)
	selection := Selection{Excludes: []string{"node_modules", ".git", ".env"}}
	preview, err := PreviewSource(context.Background(), source, selection)
	if err != nil {
		t.Fatal(err)
	}
	selection.ExpectedDigest = preview.Digest
	if err := os.WriteFile(filepath.Join(source, "new.txt"), []byte("broadened tree\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := CaptureSelectedSource(context.Background(), source, parent, testTransaction, selection); err == nil || !strings.Contains(err.Error(), "digest") {
		t.Fatalf("changed selection accepted: %v", err)
	}
	entries, err := os.ReadDir(parent)
	if err != nil || len(entries) != 0 {
		t.Fatalf("failed digest pin retained capture: %v %v", entries, err)
	}
}

func TestSelectionCanonicalAndBounded(t *testing.T) {
	encoded, err := CanonicalSelection(Selection{Excludes: []string{"node_modules", ".git", ".env"}, ExpectedDigest: strings.Repeat("a", 64)})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"excludes":[".env",".git","node_modules"],"expected_digest":"` + strings.Repeat("a", 64) + `"}`
	if encoded != want {
		t.Fatalf("canonical selection: %s", encoded)
	}
	decoded, err := ParseSelection(encoded)
	if err != nil || !reflect.DeepEqual(decoded.Excludes, []string{".env", ".git", "node_modules"}) {
		t.Fatalf("decode: %#v %v", decoded, err)
	}
	for _, invalid := range []string{`{}`, `{"excludes":null,"expected_digest":""}`, `{"excludes":[],"expected_digest":"","extra":false}`, `{"excludes":[],"expected_digest":"","excludes":[]}`, encoded + "\n"} {
		if _, err := ParseSelection(invalid); err == nil {
			t.Fatalf("ambiguous selection accepted: %s", invalid)
		}
	}
	for _, exclude := range [][]string{{"/absolute"}, {".."}, {"a/../b"}, {"a/"}, {"a", "a"}, {"a", "a/b"}, {"*.log"}, {"bad\nname"}} {
		if _, err := CanonicalSelection(Selection{Excludes: exclude}); err == nil {
			t.Fatalf("invalid exclusion accepted: %q", exclude)
		}
	}
	if _, err := CanonicalSelection(Selection{ExpectedDigest: strings.Repeat("A", 64)}); err == nil {
		t.Fatal("noncanonical digest accepted")
	}
	excludes := make([]string, 33)
	for i := range excludes {
		excludes[i] = strings.Repeat("a", i+1)
	}
	if _, err := CanonicalSelection(Selection{Excludes: excludes}); err == nil {
		t.Fatal("unbounded exclusions accepted")
	}
}

func TestPreviewFailsClosedOnUnmatchedOrUnsafeSelectedInput(t *testing.T) {
	source := selectedFixture(t)
	for _, selection := range []Selection{
		{Excludes: []string{"node_modules", ".git", ".env", "dist"}},
		{Excludes: []string{"node_modules", ".git"}},
	} {
		if _, err := PreviewSource(context.Background(), source, selection); err == nil {
			t.Fatal("unmatched exclusion or selected link accepted")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := PreviewSource(ctx, source, Selection{Excludes: []string{"node_modules", ".git", ".env"}}); err == nil {
		t.Fatal("cancelled preview accepted")
	}
}
