package main

import (
	"bytes"
	"crypto/sha256"
	"debug/elf"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// This fails if the committed installation input differs from a clean,
// reproducible static Linux/arm64 build or its checked-in digest contract.
func TestCommittedGuestHelperMatchesLockAndStaticARM64Contract(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	artifact := filepath.Join(root, "guest/ubuntu-24.04-arm64/artifacts/boxwarden-guest-bootstrap")
	first, second := filepath.Join(t.TempDir(), "first"), filepath.Join(t.TempDir(), "second")
	for _, out := range []string{first, second} {
		cmd := exec.Command("go", "build", "-trimpath", "-buildvcs=false", "-ldflags=-buildid=", "-o", out, "./cmd/boxwarden-guest-bootstrap")
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS=linux", "GOARCH=arm64")
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("build: %v\\n%s", err, output)
		}
	}
	a, err := os.ReadFile(first)
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(second)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, b) {
		t.Fatal("canonical builds differ")
	}
	committed, err := os.ReadFile(artifact)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, committed) {
		t.Fatal("committed artifact differs")
	}
	sum := sha256.Sum256(committed)
	lock, err := os.ReadFile(filepath.Join(root, "guest/ubuntu-24.04-arm64/artifacts.lock.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(lock), hex.EncodeToString(sum[:])) {
		t.Fatal("artifact digest absent from lock")
	}
	f, err := elf.Open(artifact)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if f.Machine != elf.EM_AARCH64 {
		t.Fatalf("machine = %s", f.Machine)
	}
	for _, p := range f.Progs {
		if p.Type == elf.PT_INTERP {
			t.Fatal("PT_INTERP present")
		}
	}
}
