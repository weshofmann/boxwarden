package sshx

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/weshofmann/boxwarden/internal/execx"
)

func TestExecRunnerUsesClosedUTCCLocale(t *testing.T) {
	recording := &recordingExecRunner{result: execx.Result{Stdout: "public output", Stderr: "diagnostic", Truncated: true}, err: errors.New("run failed")}
	runner := newExecRunner(recording)
	stdin := []byte("secret private key bytes")
	result, err := runner.Run(context.Background(), Command{Path: "/usr/bin/ssh-keygen", Args: []string{"-L", "-f", "/private/ca"}, Stdin: stdin})
	if !errors.Is(err, recording.err) {
		t.Fatalf("Run() error = %v, want underlying error", err)
	}
	want := []string{"LC_ALL=C", "LANG=C", "TZ=UTC"}
	if !sameStrings(recording.command.Env, want) {
		t.Fatalf("environment = %#v, want %#v", recording.command.Env, want)
	}
	if recording.command.Path != "/usr/bin/ssh-keygen" || !sameStrings(recording.command.Args, []string{"-L", "-f", "/private/ca"}) || string(recording.command.Stdin) != string(stdin) {
		t.Fatalf("forwarded command = %#v", recording.command)
	}
	if result != (Result{Stdout: "public output", Stderr: "diagnostic", Truncated: true}) {
		t.Fatalf("result = %#v", result)
	}
}

func TestExecRunnerZeroValueFailsWithoutDisclosingStdin(t *testing.T) {
	secret := "secret private key bytes"
	_, err := (ExecRunner{}).Run(context.Background(), Command{Path: "/usr/bin/ssh-keygen", Stdin: []byte(secret)})
	if err == nil {
		t.Fatal("zero-value ExecRunner error = nil")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("zero-value ExecRunner disclosed stdin in error: %v", err)
	}
}

type recordingExecRunner struct {
	command execx.Command
	result  execx.Result
	err     error
}

func (r *recordingExecRunner) Run(_ context.Context, command execx.Command) (execx.Result, error) {
	r.command = command
	return r.result, r.err
}
