//go:build (n1diagnostic || n1clipboarddiagnostic) && !n1candidate

package config

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestDiagnosticEnrollmentExactBytesAndMetadata(t *testing.T) {
	for _, mode := range []string{"good", "bytes", "symlink", "ancestor symlink", "hardlink", "mode", "size"} {
		t.Run(mode, func(t *testing.T) {
			p := filepath.Join(canonicalTempDir(t), "config.json")
			raw := []byte("synthetic nonsecret enrollment\n")
			sum := sha256.Sum256(raw)
			digest := hex.EncodeToString(sum[:])
			if err := os.WriteFile(p, raw, 0600); err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "bytes":
				if err := os.WriteFile(p, []byte("changed"), 0600); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				if err := os.Rename(p, p+".target"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(p+".target", p); err != nil {
					t.Fatal(err)
				}
			case "ancestor symlink":
				alias := filepath.Join(filepath.Dir(p), "alias")
				if err := os.Symlink(filepath.Dir(p), alias); err != nil {
					t.Fatal(err)
				}
				p = filepath.Join(alias, "config.json")
			case "hardlink":
				if err := os.Link(p, p+".alias"); err != nil {
					t.Fatal(err)
				}
			case "mode":
				if err := os.Chmod(p, 0644); err != nil {
					t.Fatal(err)
				}
			case "size":
				if err := os.WriteFile(p, make([]byte, 4097), 0600); err != nil {
					t.Fatal(err)
				}
			}
			got, err := readN1EnrollmentBytes(p, digest)
			if mode == "good" {
				if err != nil || string(got) != string(raw) {
					t.Fatalf("good=%v", err)
				}
			} else if err == nil {
				t.Fatalf("admitted %s drift", mode)
			}
		})
	}
}
func TestDiagnosticEnrollmentNoLocatorOverride(t *testing.T) {
	if _, err := LoadN1Enrollment("/private/tmp/alternate-config"); err == nil {
		t.Fatal("alternate enrollment admitted")
	}
}
