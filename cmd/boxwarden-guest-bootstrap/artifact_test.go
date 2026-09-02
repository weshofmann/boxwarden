package main

import (
	"crypto/sha256"
	"debug/elf"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCommittedGuestHelperMatchesLockAndStaticARM64Contract(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	artifact := filepath.Join(root, "guest", "ubuntu-24.04-arm64", "artifacts", "boxwarden-guest-bootstrap")
	lock := filepath.Join(root, "guest", "ubuntu-24.04-arm64", "artifacts.lock.json")
	temporary := t.TempDir()
	first, second := filepath.Join(temporary, "first"), filepath.Join(temporary, "second")
	for _, output := range []string{first, second} {
		build := exec.Command("go", "build", "-trimpath", "-buildvcs=false", "-ldflags=-buildid=", "-o", output, "./cmd/boxwarden-guest-bootstrap")
		build.Dir = root
		build.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS=linux", "GOARCH=arm64")
		if output, err := build.CombinedOutput(); err != nil {
			t.Fatalf("canonical helper build: %v\n%s", err, output)
		}
	}
	firstBytes, err := os.ReadFile(first)
	if err != nil {
		t.Fatal(err)
	}
	secondBytes, err := os.ReadFile(second)
	if err != nil {
		t.Fatal(err)
	}
	if string(firstBytes) != string(secondBytes) {
		t.Fatal("two clean canonical helper builds differ")
	}
	contents, err := os.ReadFile(artifact)
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != string(firstBytes) {
		t.Fatal("committed helper differs from canonical clean build")
	}
	sum := sha256.Sum256(contents)
	locked, err := os.ReadFile(lock)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(locked), hex.EncodeToString(sum[:])) {
		t.Fatalf("artifact digest is absent from lock")
	}
	f, err := elf.Open(artifact)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if f.Machine != elf.EM_AARCH64 {
		t.Fatalf("ELF machine = %s, want AARCH64", f.Machine)
	}
	for _, program := range f.Progs {
		if program.Type == elf.PT_INTERP {
			t.Fatal("static helper has PT_INTERP")
		}
	}
	for _, section := range f.Sections {
		if section.Type == elf.SHT_DYNAMIC {
			data, err := section.Data()
			if err != nil {
				t.Fatal(err)
			}
			for i := 0; i+16 <= len(data); i += 16 {
				if data[i] == byte(elf.DT_NEEDED) {
					t.Fatal("static helper has DT_NEEDED")
				}
			}
		}
	}
	if runtime.GOOS == "linux" && runtime.GOARCH == "arm64" && len(contents) == 0 {
		t.Fatal("empty helper")
	}
}
