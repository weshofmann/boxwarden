package importx

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProductionSourceFileCountAndLargerManifestBoundary(t *testing.T) {
	source := privateDirectory(t)
	for i := 0; i < MaxFiles; i++ {
		name := fmt.Sprintf("f%04d-%s", i, strings.Repeat("a", 50))
		if err := os.WriteFile(filepath.Join(source, name), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	preview, err := PreviewSource(t.Context(), source, Selection{})
	if err != nil || preview.FileCount != MaxFiles {
		t.Fatalf("file boundary: %d %v", preview.FileCount, err)
	}
	manifest, err := json.Marshal(struct {
		Version int     `json:"version"`
		Entries []Entry `json:"entries"`
	}{1, preview.Entries})
	if err != nil || len(manifest) <= 256<<10 {
		t.Fatalf("manifest did not exceed old bound: %d %v", len(manifest), err)
	}
	parent := privateDirectory(t)
	if err := os.Rename(source, filepath.Join(parent, testTransaction)); err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(parent, testTransaction)
	if err := os.WriteFile(filepath.Join(directory, manifestName), append(manifest, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	snapshot, err := InspectSnapshot(parent, testTransaction)
	if err != nil || snapshot.FileCount != MaxFiles || snapshot.Digest != preview.Digest {
		t.Fatalf("larger canonical manifest rejected: %d %v", snapshot.FileCount, err)
	}
	if err := os.WriteFile(filepath.Join(directory, "overflow.txt"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	// Preview a regular source again, excluding only the synthetic snapshot metadata.
	if _, err := PreviewSource(t.Context(), directory, Selection{Excludes: []string{manifestName}}); err == nil {
		t.Fatal("file count overflow accepted")
	}
	if err := os.Truncate(filepath.Join(directory, manifestName), (4<<20)+1); err != nil {
		t.Fatal(err)
	}
	if _, err := InspectSnapshot(parent, testTransaction); err == nil {
		t.Fatal("manifest metadata overflow accepted")
	}
}

func TestProductionSourceDirectoryCountBoundary(t *testing.T) {
	source := privateDirectory(t)
	if err := os.WriteFile(filepath.Join(source, "README.md"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < MaxDirectories; i++ {
		if err := os.Mkdir(filepath.Join(source, fmt.Sprintf("d%04d", i)), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	preview, err := PreviewSource(t.Context(), source, Selection{})
	if err != nil || preview.DirectoryCount != MaxDirectories {
		t.Fatalf("directory boundary: %d %v", preview.DirectoryCount, err)
	}
	if err := os.Mkdir(filepath.Join(source, "overflow"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := PreviewSource(t.Context(), source, Selection{}); err == nil {
		t.Fatal("directory count overflow accepted")
	}
}

func TestProductionSourceFileAndTotalByteBoundaries(t *testing.T) {
	source := privateDirectory(t)
	for i := 0; i < 4; i++ {
		file, err := os.OpenFile(filepath.Join(source, fmt.Sprintf("f%d", i)), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		err = file.Truncate(MaxFileBytes)
		closeErr := file.Close()
		if err != nil || closeErr != nil {
			t.Fatalf("sparse fixture: %v %v", err, closeErr)
		}
	}
	preview, err := PreviewSource(t.Context(), source, Selection{})
	if err != nil || preview.TotalBytes != MaxTotalBytes {
		t.Fatalf("file/total boundary: %d %v", preview.TotalBytes, err)
	}
	if err := os.WriteFile(filepath.Join(source, "overflow.txt"), []byte{1}, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := PreviewSource(t.Context(), source, Selection{}); err == nil {
		t.Fatal("total byte overflow accepted")
	}
	if err := os.Remove(filepath.Join(source, "overflow.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(filepath.Join(source, "f0"), MaxFileBytes+1); err != nil {
		t.Fatal(err)
	}
	if _, err := PreviewSource(t.Context(), source, Selection{}); err == nil {
		t.Fatal("file byte overflow accepted")
	}
}
