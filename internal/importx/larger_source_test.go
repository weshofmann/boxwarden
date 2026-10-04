package importx

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestCaptureAndInspectSourceBeyondTinyDemoLimits(t *testing.T) {
	for _, tc := range []struct {
		name  string
		files int
		bytes int64
	}{
		{"300 files", 300, 1},
		{"five MiB file", 1, 5 << 20},
		{"twenty MiB tree", 5, 4 << 20},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source, parent := privateDirectory(t), privateDirectory(t)
			for i := 0; i < tc.files; i++ {
				name := filepath.Join(source, fmt.Sprintf("file%04d", i))
				f, err := os.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
				if err != nil {
					t.Fatal(err)
				}
				err = f.Truncate(tc.bytes)
				closeErr := f.Close()
				if err != nil || closeErr != nil {
					t.Fatalf("fixture: %v %v", err, closeErr)
				}
			}
			got, err := CaptureSource(context.Background(), source, parent, testTransaction)
			if err != nil {
				t.Fatal(err)
			}
			if got.FileCount != tc.files || got.TotalBytes != int64(tc.files)*tc.bytes {
				t.Fatalf("capture metadata: %#v", got)
			}
			inspected, err := InspectSnapshot(parent, testTransaction)
			if err != nil || inspected.Digest != got.Digest || inspected.FileCount != tc.files || inspected.TotalBytes != got.TotalBytes {
				t.Fatalf("snapshot re-admission: %#v %v", inspected, err)
			}
			for i := 0; i < tc.files; i++ {
				info, err := os.Stat(filepath.Join(source, fmt.Sprintf("file%04d", i)))
				if err != nil || info.Size() != tc.bytes || info.Mode().Perm() != 0o600 {
					t.Fatalf("original changed: %v %v", info, err)
				}
			}
		})
	}
}
