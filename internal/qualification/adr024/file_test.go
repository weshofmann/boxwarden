//go:build darwin || linux

package adr024

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestReadAdmittedFileBindsDirectFileMetadataAndBytes(t *testing.T) {
	directory, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "artifact")
	contents := []byte("qualified artifact\n")
	if err := os.WriteFile(path, contents, 0o444); err != nil {
		t.Fatal(err)
	}
	digest := fmt.Sprintf("%x", sha256.Sum256(contents))
	evidence, raw, err := readAdmittedFile(path, filePolicy{
		UID: os.Getuid(), GID: os.Getgid(), Mode: 0o444, Links: 1,
		SHA256: digest, MaxBytes: 1024,
	})
	if err != nil {
		t.Fatalf("readAdmittedFile() error = %v", err)
	}
	if string(raw) != string(contents) || evidence.Path != path || evidence.SHA256 != digest || evidence.Links != 1 || evidence.Size != int64(len(contents)) || evidence.ModifiedUnixNanos <= 0 {
		t.Fatalf("evidence = %#v raw = %q", evidence, raw)
	}

	link := filepath.Join(directory, "link")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, _, err := readAdmittedFile(link, filePolicy{UID: os.Getuid(), GID: os.Getgid(), Mode: 0o444, Links: 1, SHA256: digest, MaxBytes: 1024}); err == nil {
		t.Fatal("symlinked artifact accepted")
	}
	if _, _, err := readAdmittedFile(path, filePolicy{UID: os.Getuid(), GID: os.Getgid(), Mode: 0o444, Links: 1, SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", MaxBytes: 1024}); err == nil {
		t.Fatal("artifact with wrong digest accepted")
	}
}

func TestReadAdmittedFileRejectsMetadataLinksBoundsAndNoncanonicalPaths(t *testing.T) {
	directory, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "artifact")
	contents := []byte("qualified artifact\n")
	if err := os.WriteFile(path, contents, 0o444); err != nil {
		t.Fatal(err)
	}
	digest := fmt.Sprintf("%x", sha256.Sum256(contents))
	valid := filePolicy{UID: os.Getuid(), GID: os.Getgid(), Mode: 0o444, Links: 1, SHA256: digest, MaxBytes: 1024}

	tests := []struct {
		name   string
		path   string
		policy filePolicy
	}{
		{name: "wrong UID", path: path, policy: func() filePolicy { p := valid; p.UID++; return p }()},
		{name: "wrong GID", path: path, policy: func() filePolicy { p := valid; p.GID++; return p }()},
		{name: "wrong mode", path: path, policy: func() filePolicy { p := valid; p.Mode = 0o400; return p }()},
		{name: "oversize", path: path, policy: func() filePolicy { p := valid; p.MaxBytes = int64(len(contents) - 1); return p }()},
		{name: "noncanonical path", path: directory + "/./artifact", policy: valid},
		{name: "missing expected digest", path: path, policy: func() filePolicy { p := valid; p.SHA256 = ""; return p }()},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, _, err := readAdmittedFile(test.path, test.policy); err == nil {
				t.Fatal("readAdmittedFile() error = nil")
			}
		})
	}

	hardLink := filepath.Join(directory, "hard-link")
	if err := os.Link(path, hardLink); err != nil {
		t.Fatal(err)
	}
	if _, _, err := readAdmittedFile(path, valid); err == nil {
		t.Fatal("multiply linked artifact accepted")
	}
}
