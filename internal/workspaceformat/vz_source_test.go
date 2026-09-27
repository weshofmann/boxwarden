package workspaceformat

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCleanSourceCommitRequiresExactCleanCheckout(t *testing.T) {
	root := t.TempDir()
	runGit := func(args ...string) {
		t.Helper()
		command := exec.Command("/usr/bin/git", append([]string{"-C", root}, args...)...)
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, output)
		}
	}
	runGit("init")
	if err := os.WriteFile(filepath.Join(root, "source.txt"), []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit("add", "source.txt")
	runGit("-c", "user.name=Boxwarden Test", "-c", "user.email=boxwarden@example.invalid", "commit", "-m", "fixture")
	commit, err := cleanSourceCommit(t.Context(), root)
	if err != nil || !lowerHex(commit, 40) {
		t.Fatalf("clean source failed: %q, %v", commit, err)
	}
	if err := os.WriteFile(filepath.Join(root, "untracked.txt"), []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := cleanSourceCommit(t.Context(), root); err == nil {
		t.Fatal("untracked source accepted")
	}
	if err := os.Remove(filepath.Join(root, "untracked.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "source.txt"), []byte("modified"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := cleanSourceCommit(t.Context(), root); err == nil {
		t.Fatal("modified source accepted")
	}
	if _, err := cleanSourceCommit(t.Context(), root+"/."); err == nil || !strings.Contains(err.Error(), "clean and absolute") {
		t.Fatalf("unclean path accepted: %v", err)
	}
}
