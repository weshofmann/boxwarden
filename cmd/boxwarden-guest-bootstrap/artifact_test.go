package main

import (
	"bytes"
	"crypto/sha256"
	"debug/elf"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

type artifactLock struct {
	Version   int                       `json:"version"`
	Artifacts map[string]artifactRecord `json:"artifacts"`
}
type artifactRecord struct {
	SHA256    string `json:"sha256"`
	GoVersion string `json:"go_version"`
	Build     string `json:"build"`
	ELF       struct {
		Machine  string `json:"machine"`
		PTInterp bool   `json:"pt_interp"`
		DTNeeded bool   `json:"dt_needed"`
	} `json:"elf"`
}

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
	parsed, err := decodeExactArtifactLock(lock)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Version != 1 || len(parsed.Artifacts) != 1 {
		t.Fatalf("lock topology = %#v", parsed)
	}
	record, ok := parsed.Artifacts["boxwarden-guest-bootstrap"]
	if !ok {
		t.Fatal("named helper missing from lock")
	}
	wantBuild := "CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -buildvcs=false -ldflags=-buildid= -o guest/ubuntu-24.04-arm64/artifacts/boxwarden-guest-bootstrap ./cmd/boxwarden-guest-bootstrap"
	if record.SHA256 != hex.EncodeToString(sum[:]) || record.GoVersion != runtime.Version() || record.Build != wantBuild || record.ELF.Machine != "EM_AARCH64" || record.ELF.PTInterp || record.ELF.DTNeeded {
		t.Fatalf("invalid exact artifact lock record: %#v", record)
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
		if p.Type == elf.PT_DYNAMIC {
			t.Fatal("PT_DYNAMIC present")
		}
	}
	if libraries, err := f.ImportedLibraries(); err != nil || len(libraries) != 0 {
		t.Fatalf("imported libraries = %v, %v", libraries, err)
	}
	userData, err := os.ReadFile(filepath.Join(root, "guest/ubuntu-24.04-arm64/autoinstall/user-data"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(userData), record.SHA256) || !strings.Contains(string(userData), "/cdrom/boxwarden-artifacts/boxwarden-guest-bootstrap") {
		t.Fatal("autoinstall does not bind the locked artifact digest and input path")
	}
}

func decodeExactArtifactLock(data []byte) (artifactLock, error) {
	fields, err := exactJSONFields(data, "version", "artifacts")
	if err != nil {
		return artifactLock{}, err
	}
	var version int
	if err := json.Unmarshal(fields["version"], &version); err != nil {
		return artifactLock{}, err
	}
	artifacts, err := exactJSONFields(fields["artifacts"], "boxwarden-guest-bootstrap")
	if err != nil {
		return artifactLock{}, err
	}
	recordFields, err := exactJSONFields(artifacts["boxwarden-guest-bootstrap"], "sha256", "go_version", "build", "elf")
	if err != nil {
		return artifactLock{}, err
	}
	elfFields, err := exactJSONFields(recordFields["elf"], "machine", "pt_interp", "dt_needed")
	if err != nil {
		return artifactLock{}, err
	}
	var record artifactRecord
	if err := json.Unmarshal(mustJSON(recordFields), &record); err != nil {
		return artifactLock{}, err
	}
	if err := json.Unmarshal(mustJSON(elfFields), &record.ELF); err != nil {
		return artifactLock{}, err
	}
	return artifactLock{Version: version, Artifacts: map[string]artifactRecord{"boxwarden-guest-bootstrap": record}}, nil
}

func exactJSONFields(data []byte, allowed ...string) (map[string]json.RawMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return nil, fmt.Errorf("expected object")
	}
	permitted := map[string]bool{}
	for _, key := range allowed {
		permitted[key] = true
	}
	fields := map[string]json.RawMessage{}
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		key, ok := token.(string)
		if !ok || !permitted[key] || fields[key] != nil {
			return nil, fmt.Errorf("unknown or duplicate lock field")
		}
		var raw json.RawMessage
		if err := decoder.Decode(&raw); err != nil {
			return nil, err
		}
		fields[key] = raw
	}
	if _, err := decoder.Token(); err != nil {
		return nil, err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return nil, fmt.Errorf("trailing lock data")
	}
	for _, key := range allowed {
		if fields[key] == nil {
			return nil, fmt.Errorf("missing lock field %q", key)
		}
	}
	return fields, nil
}

func mustJSON(fields map[string]json.RawMessage) []byte {
	data, err := json.Marshal(fields)
	if err != nil {
		panic(err)
	}
	return data
}
