package hostx

import (
	"context"
	"errors"
	"testing"

	"github.com/weshofmann/boxwarden/internal/execx"
)

func TestDetectPasswordlessRuleBindsExactMutableHomebrewSoftnet(t *testing.T) {
	target := "/opt/homebrew/bin/softnet"
	for name, test := range map[string]struct {
		output string
		want   bool
	}{
		"exact rule":        {"(root) NOPASSWD: /opt/homebrew/bin/softnet --flag\n", true},
		"passworded rule":   {"(root) PASSWD: /opt/homebrew/bin/softnet\n", false},
		"other command":     {"(root) NOPASSWD: /usr/bin/id\n", false},
		"prefix collision":  {"(root) NOPASSWD: /opt/homebrew/bin/softnet-helper\n", false},
		"homebrew wildcard": {"(root) NOPASSWD: /opt/homebrew/bin/*\n", true},
		"all commands":      {"(root) NOPASSWD: ALL\n", true},
	} {
		t.Run(name, func(t *testing.T) {
			if got := detectPasswordlessRule(test.output, target); got != test.want {
				t.Fatalf("detectPasswordlessRule() = %t, want %t", got, test.want)
			}
		})
	}
}

func TestPasswordlessPolicyInspectionFailsClosedWhenSudoCannotProvePolicy(t *testing.T) {
	inspector := &osDoctorInspector{runner: policyRunnerFake{result: execx.Result{Stderr: "sudo: a password is required\n"}, err: errors.New("exit status 1")}}
	if _, err := inspector.passwordlessRoot("/opt/homebrew/bin/softnet"); err == nil {
		t.Fatal("passwordlessRoot(unavailable policy) error = nil, want fail-closed result")
	}
}

type policyRunnerFake struct {
	result execx.Result
	err    error
}

func (f policyRunnerFake) Run(context.Context, execx.Command) (execx.Result, error) {
	return f.result, f.err
}
