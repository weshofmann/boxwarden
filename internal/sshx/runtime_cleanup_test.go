package sshx

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCleanupGenerationCredentialsRemovesOnlyFixedAdmittedFiles(t *testing.T) {
	runtime := privateRoot(t)
	for name, mode := range map[string]os.FileMode{"client": 0o600, "client.pub": 0o644, "client-cert.pub": 0o644, "known_hosts": 0o600} {
		mustWrite(t, filepath.Join(runtime, name), []byte("fixture"), mode)
	}
	if err := CleanupGenerationCredentials(runtime); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"client", "client.pub", "client-cert.pub", "known_hosts"} {
		if _, err := os.Lstat(filepath.Join(runtime, name)); !os.IsNotExist(err) {
			t.Fatalf("%s remains: %v", name, err)
		}
	}
	if err := CleanupGenerationCredentials(runtime); err != nil {
		t.Fatalf("idempotent cleanup: %v", err)
	}
}

func TestCleanupGenerationCredentialsRefusesChangedPath(t *testing.T) {
	runtime := privateRoot(t)
	outside := filepath.Join(privateRoot(t), "sentinel")
	mustWrite(t, outside, []byte("keep"), 0o600)
	if err := os.Symlink(outside, filepath.Join(runtime, "client")); err != nil {
		t.Fatal(err)
	}
	if err := CleanupGenerationCredentials(runtime); err == nil {
		t.Fatal("followed symlink during credential cleanup")
	}
	if contents, err := os.ReadFile(outside); err != nil || string(contents) != "keep" {
		t.Fatalf("foreign file changed: %q, %v", contents, err)
	}
}
