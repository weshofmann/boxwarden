package hostx

import (
	"errors"
	"fmt"
	"os/exec"
	"testing"

	"github.com/weshofmann/boxwarden/internal/execx"
)

func TestOSDoctorCommandOutputAcceptsKnownScreenVersionStatusOne(t *testing.T) {
	exitStatusOne := exitStatusOneError(t)
	inspector := &osDoctorInspector{runner: policyRunnerFake{
		result: execx.Result{Stdout: ScreenVersionOutput + "\n"},
		err:    fmt.Errorf("run %q: %w", ScreenPath, exitStatusOne),
	}}

	got, err := inspector.CommandOutput(ScreenPath, "--version")
	if err != nil {
		t.Fatalf("CommandOutput() error = %v", err)
	}
	if got != ScreenVersionOutput {
		t.Fatalf("CommandOutput() = %q, want %q", got, ScreenVersionOutput)
	}
}

func TestOSDoctorCommandOutputRejectsSyntheticScreenStatusOneError(t *testing.T) {
	inspector := &osDoctorInspector{runner: policyRunnerFake{
		result: execx.Result{Stdout: ScreenVersionOutput + "\n"},
		err:    errors.New("exit status 1"),
	}}

	if _, err := inspector.CommandOutput(ScreenPath, "--version"); err == nil {
		t.Fatal("CommandOutput() error = nil, want synthetic error rejection")
	}
}

func TestOSDoctorCommandOutputRejectsScreenVersionWarningOnStderr(t *testing.T) {
	exitStatusOne := exitStatusOneError(t)
	inspector := &osDoctorInspector{runner: policyRunnerFake{
		result: execx.Result{Stdout: ScreenVersionOutput + "\n", Stderr: "warning\n"},
		err:    fmt.Errorf("run %q: %w", ScreenPath, exitStatusOne),
	}}

	if _, err := inspector.CommandOutput(ScreenPath, "--version"); err == nil {
		t.Fatal("CommandOutput() error = nil, want warning stderr rejection")
	}
}

func TestOSDoctorCommandOutputAcceptsScreenVersionWhitespaceOnlyStderr(t *testing.T) {
	exitStatusOne := exitStatusOneError(t)
	inspector := &osDoctorInspector{runner: policyRunnerFake{
		result: execx.Result{Stdout: ScreenVersionOutput + "\n", Stderr: " \t\n"},
		err:    fmt.Errorf("run %q: %w", ScreenPath, exitStatusOne),
	}}
	if _, err := inspector.CommandOutput(ScreenPath, "--version"); err != nil {
		t.Fatalf("CommandOutput() error = %v", err)
	}
}

func TestOSDoctorCommandOutputRejectsOtherFailedCommands(t *testing.T) {
	inspector := &osDoctorInspector{runner: policyRunnerFake{
		result: execx.Result{Stdout: "Screen version 4.00.03 (FAU) 23-Oct-06\n"},
		err:    errors.New("exit status 1"),
	}}

	if _, err := inspector.CommandOutput("/usr/bin/ssh", "-V"); err == nil {
		t.Fatal("CommandOutput() error = nil, want failed command rejection")
	}
}

func TestOSDoctorCommandOutputRejectsUnexpectedScreenVersionFailure(t *testing.T) {
	inspector := &osDoctorInspector{runner: policyRunnerFake{
		result: execx.Result{Stdout: ScreenVersionOutput + "\n"},
		err:    errors.New("exit status 2"),
	}}

	if _, err := inspector.CommandOutput(ScreenPath, "--version"); err == nil {
		t.Fatal("CommandOutput() error = nil, want unexpected screen failure rejection")
	}
}

func exitStatusOneError(t *testing.T) *exec.ExitError {
	t.Helper()
	err := exec.Command("/usr/bin/false").Run()
	var exitError *exec.ExitError
	if !errors.As(err, &exitError) || exitError.ExitCode() != 1 {
		t.Fatalf("false error = %v, want wrapped exit status 1", err)
	}
	return exitError
}
