package sshx

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/weshofmann/boxwarden/internal/domain"
)

const (
	testUUID         = "123e4567-e89b-42d3-a456-426614174000"
	testPublicKey    = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	changedPublicKey = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAB"
)

type fakeRunner struct {
	commands []Command
	onRun    func(Command) Result
	err      error
}

func (r *fakeRunner) Run(_ context.Context, command Command) (Result, error) {
	r.commands = append(r.commands, command)
	if r.onRun != nil {
		return r.onRun(command), r.err
	}
	return Result{}, r.err
}

func newKeygenRunner(t *testing.T) *fakeRunner {
	t.Helper()
	return &fakeRunner{onRun: func(command Command) Result {
		if len(command.Args) > 0 && command.Args[0] == "-q" {
			mustWrite(t, command.Args[4], []byte("private-ca"), 0o600)
			mustWrite(t, command.Args[4]+".pub", []byte(testPublicKey+" boxwarden-ca\n"), 0o600)
		}
		if len(command.Args) > 0 && command.Args[0] == "-y" {
			return Result{Stdout: testPublicKey + " boxwarden-ca\n"}
		}
		if len(command.Args) > 0 && command.Args[0] == "-s" {
			mustWrite(t, command.Args[len(command.Args)-1]+"-cert.pub", []byte("certificate\n"), 0o644)
		}
		if len(command.Args) > 0 && command.Args[0] == "-L" {
			return Result{Stdout: "Type: ssh-ed25519-cert-v01@openssh.com user certificate\nKey ID: \"boxwarden:work:" + testUUID + "\"\nValid: from 2026-09-01T11:55:00 to 2026-09-01T12:15:00\nPrincipals:\n        boxwarden-session-" + testUUID + "\nCritical Options: (none)\nExtensions: (none)\n"}
		}
		return Result{}
	}}
}

func privateRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	return root
}

func mustWrite(t *testing.T, path string, contents []byte, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, contents, mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func sameStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func fileAt(root string, elems ...string) string {
	return filepath.Join(append([]string{root}, elems...)...)
}

func testBinding(t *testing.T, value Domain) Binding {
	t.Helper()
	if _, err := domain.Parse(string(value.ID)); err != nil {
		t.Fatal(err)
	}
	return Binding{Domain: value.ID, SessionID: testUUID, BackendKind: "tart", BackendObject: "workstation"}
}
