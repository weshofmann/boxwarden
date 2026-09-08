package hostx

import (
	"errors"
	"testing"

	"github.com/weshofmann/boxwarden/internal/execx"
)

func TestOSDoctorCommandOutputRejectsFailedCommands(t *testing.T) {
	inspector := &osDoctorInspector{runner: policyRunnerFake{
		result: execx.Result{Stdout: "tool version"},
		err:    errors.New("exit status 1"),
	}}
	if _, err := inspector.CommandOutput("/usr/bin/ssh", "-V"); err == nil {
		t.Fatal("CommandOutput() error = nil, want failed command rejection")
	}
}
