package tart

import (
	"context"
	"testing"

	"github.com/weshofmann/boxwarden/internal/execx"
)

func TestAddressResolverReturnsOneLiteralIP(t *testing.T) {
	runner := &lifecycleRecordingRunner{result: execx.Result{Stdout: "192.0.2.10\n"}}
	resolver := NewAddressResolver(runner, "/opt/qualified/tart", "/Users/wes/.boxwarden/tart")

	got, err := resolver.Resolve(context.Background(), "boxwarden-work-dev")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if got != "192.0.2.10" {
		t.Fatalf("Resolve() = %q, want one literal IP", got)
	}
	if got, want := runner.command.Path, "/opt/qualified/tart"; got != want {
		t.Fatalf("command path = %q, want %q", got, want)
	}
	if got, want := runner.command.Args, []string{"ip", "--resolver=dhcp", "--wait=60", "boxwarden-work-dev"}; !sameLifecycleStrings(got, want) {
		t.Fatalf("command args = %#v, want %#v", got, want)
	}
	if got, want := runner.command.Env, []string{"TART_HOME=/Users/wes/.boxwarden/tart", "LANG=C", "LC_ALL=C"}; !sameLifecycleStrings(got, want) {
		t.Fatalf("command environment = %#v, want %#v", got, want)
	}
	if !runner.deadline {
		t.Fatal("Resolve() did not use a bounded command context")
	}
	for _, output := range []string{"", "192.0.2.10\n192.0.2.11\n", "guest.local\n", "192.0.2.10:22\n"} {
		runner.result = execx.Result{Stdout: output}
		if _, err := resolver.Resolve(context.Background(), "boxwarden-work-dev"); err == nil {
			t.Fatalf("Resolve() with %q error = nil, want literal-IP refusal", output)
		}
	}
}

type lifecycleRecordingRunner struct {
	command  execx.Command
	result   execx.Result
	err      error
	deadline bool
}

func (r *lifecycleRecordingRunner) Run(ctx context.Context, command execx.Command) (execx.Result, error) {
	r.command = command
	_, r.deadline = ctx.Deadline()
	return r.result, r.err
}
