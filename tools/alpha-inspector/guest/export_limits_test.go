package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGuestRequestAdmitsFourGiBDiskAndRejectsAbove(t *testing.T) {
	tx := [16]byte{1}
	for _, tc := range []struct {
		size int64
		pass bool
	}{{(1 << 30) + 512, true}, {4 << 30, true}, {(4 << 30) + 512, false}} {
		raw := []byte(fmt.Sprintf(`{"version":1,"transaction":"01000000000000000000000000000000","filesystem_uuid":"2f1c6b88-9849-4c5d-9d20-f3bc30bd77a1","disk_bytes":%d,"selected":["project"]}`, tc.size))
		request, err := decodeExportRequest(raw, tx)
		if (err == nil) != tc.pass || tc.pass && request.DiskBytes != tc.size {
			t.Fatalf("disk request %d=%+v, %v", tc.size, request, err)
		}
	}
}

func TestGuestDefaultsExportAbove256MiBThroughBoundedWriter(t *testing.T) {
	root := t.TempDir()
	for i, size := range []int64{128 << 20, (128 << 20) + 1} {
		file, err := os.OpenFile(filepath.Join(root, fmt.Sprintf("f%d", i)), os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		if err := file.Truncate(size); err != nil {
			t.Fatal(err)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
	}
	total, err := writeSelectedExportBody(context.Background(), io.Discard, root, [16]byte{1}, []string{"f0", "f1"}, defaultGuestExportLimits)
	if err != nil || total != (256<<20)+1 {
		t.Fatalf("larger selected aggregate=%d, %v", total, err)
	}
	var terminal bytes.Buffer
	if err := writeExportTerminal(&terminal, 512<<20); err != nil {
		t.Fatalf("512 MiB terminal rejected: %v", err)
	}
	before := terminal.Len()
	if err := writeExportTerminal(&terminal, (512<<20)+1); err == nil || terminal.Len() != before {
		t.Fatal("oversized terminal emitted")
	}
}

func TestGuestDefaultFileAdmissionAtLargerCountAndTotal(t *testing.T) {
	path := t.TempDir()
	if err := os.WriteFile(filepath.Join(path, "empty"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "one"), []byte{1}, 0o600); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	for _, tc := range []struct {
		name  string
		files int
		total int64
		file  string
		pass  bool
	}{
		{"beyond old count", 4096, 0, "empty", true},
		{"last bounded file", 8191, 0, "empty", true},
		{"too many files", 8192, 0, "empty", false},
		{"last bounded byte", 0, (512 << 20) - 1, "one", true},
		{"too many bytes", 0, 512 << 20, "one", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			info, err := root.Stat(tc.file)
			if err != nil {
				t.Fatal(err)
			}
			var stream bytes.Buffer
			writer := guestExportWriter{ctx: context.Background(), root: root, output: &stream, limits: defaultGuestExportLimits, seen: map[string]string{}, files: tc.files, total: tc.total}
			err = writer.exportFile(tc.file, info)
			if (err == nil) != tc.pass {
				t.Fatalf("file admission=%v, pass=%t", err, tc.pass)
			}
			if !tc.pass && stream.Len() != 0 {
				t.Fatal("rejected file emitted bytes")
			}
			if tc.pass && (writer.files != tc.files+1 || writer.total != tc.total+info.Size()) {
				t.Fatalf("successful file lost counters: files=%d,total=%d", writer.files, writer.total)
			}
		})
	}
	file, err := os.OpenFile(filepath.Join(path, "too-large"), os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate((256 << 20) + 1); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	var stream bytes.Buffer
	if _, err := writeSelectedExportBody(context.Background(), &stream, path, [16]byte{1}, []string{"too-large"}, defaultGuestExportLimits); err == nil || !strings.Contains(err.Error(), "file size or count") || stream.Len() != 22 {
		t.Fatalf("per-file guard emitted oversized file: bytes=%d, err=%v", stream.Len(), err)
	}
}
