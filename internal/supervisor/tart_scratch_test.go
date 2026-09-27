package supervisor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerationAdmitsOnlyPrivateExactTartScratch(t *testing.T) {
	request := minimalRequest(t)
	if _, _, err := publishOrAdmitRequest(request); err != nil {
		t.Fatal(err)
	}
	scratch := filepath.Join(request.RuntimeDirectory, "tart")
	if err := os.Mkdir(scratch, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := admitGeneration(request); err != nil {
		t.Fatalf("private empty Tart scratch rejected: %v", err)
	}
	if err := os.WriteFile(filepath.Join(scratch, "foreign"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := admitGeneration(request); err == nil || !strings.Contains(err.Error(), "Tart scratch") {
		t.Fatalf("foreign Tart scratch entry admitted: %v", err)
	}
	if err := os.Remove(filepath.Join(scratch, "foreign")); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(scratch, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := admitGeneration(request); err == nil {
		t.Fatal("nonprivate Tart scratch admitted")
	}
}

func TestGenerationRejectsTartSocketAtRoot(t *testing.T) {
	request := minimalRequest(t)
	if _, _, err := publishOrAdmitRequest(request); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(request.RuntimeDirectory, "control.sock"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := admitGeneration(request); err == nil || !strings.Contains(err.Error(), "unexpected generation entry") {
		t.Fatalf("root Tart socket admitted: %v", err)
	}
}
