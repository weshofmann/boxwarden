package sshx

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureClientKeyCreatesAndRevalidatesExactGenerationKey(t *testing.T) {
	runtime := privateRoot(t)
	runner := &fakeRunner{onRun: func(command Command) Result {
		switch command.Args[0] {
		case "-q":
			mustWrite(t, filepath.Join(runtime, "client"), []byte("private-key"), 0o600)
			mustWrite(t, filepath.Join(runtime, "client.pub"), []byte(testPublicKey+" generated\n"), 0o600)
		case "-y":
			return Result{Stdout: testPublicKey + " generated\n"}
		}
		return Result{}
	}}
	path, err := EnsureClientKey(context.Background(), runner, runtime)
	if err != nil || path != filepath.Join(runtime, "client") {
		t.Fatalf("EnsureClientKey() = %q, %v", path, err)
	}
	if _, err := EnsureClientKey(context.Background(), runner, runtime); err != nil {
		t.Fatalf("revalidate existing key: %v", err)
	}
	if info, err := os.Lstat(filepath.Join(runtime, "client.pub")); err != nil || info.Mode().Perm() != 0o644 {
		t.Fatalf("generated public key mode = %v, %v", info, err)
	}
	if len(runner.commands) != 3 || runner.commands[0].Args[0] != "-q" || runner.commands[1].Args[0] != "-y" || runner.commands[2].Args[0] != "-y" {
		t.Fatalf("generation/revalidation commands = %#v", runner.commands)
	}
}

func TestEnsureClientKeyRejectsPartialAndConflictingMaterial(t *testing.T) {
	for _, test := range []struct {
		name   string
		setup  func(*testing.T, string)
		output string
	}{
		{name: "partial private", setup: func(t *testing.T, root string) { mustWrite(t, filepath.Join(root, "client"), []byte("key"), 0o600) }, output: testPublicKey},
		{name: "partial public", setup: func(t *testing.T, root string) {
			mustWrite(t, filepath.Join(root, "client.pub"), []byte(testPublicKey+"\n"), 0o644)
		}, output: testPublicKey},
		{name: "mismatch", setup: func(t *testing.T, root string) {
			mustWrite(t, filepath.Join(root, "client"), []byte("key"), 0o600)
			mustWrite(t, filepath.Join(root, "client.pub"), []byte(testPublicKey+"\n"), 0o644)
		}, output: changedPublicKey},
	} {
		t.Run(test.name, func(t *testing.T) {
			runtime := privateRoot(t)
			test.setup(t, runtime)
			runner := &fakeRunner{onRun: func(Command) Result { return Result{Stdout: test.output + "\n"} }}
			if _, err := EnsureClientKey(context.Background(), runner, runtime); err == nil {
				t.Fatal("unsafe key state accepted")
			}
			if test.name != "mismatch" && len(runner.commands) != 0 {
				t.Fatalf("ran keygen on partial state: %#v", runner.commands)
			}
		})
	}
}

func TestEnsureClientKeyRejectsNonPrivateRuntime(t *testing.T) {
	runtime := t.TempDir()
	if err := os.Chmod(runtime, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := EnsureClientKey(context.Background(), &fakeRunner{}, runtime); err == nil {
		t.Fatal("accepted nonprivate generation")
	}
}
