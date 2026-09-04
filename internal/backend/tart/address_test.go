package tart

import (
	"context"
	"strings"
	"testing"

	"github.com/weshofmann/boxwarden/internal/execx"
)

func TestAddressResolverUsesExactDHCPRefreshCommand(t *testing.T) {
	runner := &addressRecordingRunner{result: execx.Result{Stdout: "192.0.2.10\n"}}
	resolver := NewAddressResolver(runner, "/opt/qualified/tart")

	address, err := resolver.Resolve(context.Background(), "boxwarden-work-dev")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if address != "192.0.2.10" {
		t.Fatalf("Resolve() = %q, want literal address", address)
	}
	if got, want := runner.command.Path, "/opt/qualified/tart"; got != want {
		t.Fatalf("command path = %q, want %q", got, want)
	}
	if got, want := runner.command.Args, []string{"ip", "--resolver=dhcp", "--wait=60", "boxwarden-work-dev"}; !sameStrings(got, want) {
		t.Fatalf("command args = %#v, want %#v", got, want)
	}
	if !runner.deadline {
		t.Fatal("Resolve() did not bound Tart ip")
	}
}

func TestAddressResolverRejectsAmbiguousAddressOutput(t *testing.T) {
	for name, output := range map[string]string{
		"empty": "", "multiple": "192.0.2.10\n192.0.2.11\n", "hostname": "guest.local\n", "IPv4 with port": "192.0.2.10:22\n",
	} {
		t.Run(name, func(t *testing.T) {
			resolver := NewAddressResolver(&addressRecordingRunner{result: execx.Result{Stdout: output}}, "/opt/qualified/tart")
			if _, err := resolver.Resolve(context.Background(), "boxwarden-work-dev"); err == nil {
				t.Fatal("Resolve() error = nil, want literal-IP rejection")
			}
		})
	}
}

func TestAddressResolverReturnsEachFreshLookupResult(t *testing.T) {
	runner := &addressRecordingRunner{responses: []execx.Result{{Stdout: "192.0.2.10\n"}, {Stdout: "192.0.2.11\n"}}}
	resolver := NewAddressResolver(runner, "/opt/qualified/tart")
	first, err := resolver.Resolve(context.Background(), "boxwarden-work-dev")
	if err != nil {
		t.Fatal(err)
	}
	second, err := resolver.Resolve(context.Background(), "boxwarden-work-dev")
	if err != nil {
		t.Fatal(err)
	}
	if first != "192.0.2.10" || second != "192.0.2.11" || runner.calls != 2 {
		t.Fatalf("fresh resolution = %q, %q after %d calls", first, second, runner.calls)
	}
}

func TestAddressResolverRejectsTruncatedOrOversizedOutput(t *testing.T) {
	for name, result := range map[string]execx.Result{
		"truncated":        {Stdout: "192.0.2.10\n", Truncated: true},
		"oversized stderr": {Stdout: "192.0.2.10\n", Stderr: strings.Repeat("x", maxAddressBytes+1)},
	} {
		t.Run(name, func(t *testing.T) {
			resolver := NewAddressResolver(&addressRecordingRunner{result: result}, "/opt/qualified/tart")
			if _, err := resolver.Resolve(context.Background(), "boxwarden-work-dev"); err == nil {
				t.Fatal("Resolve() error = nil, want bounded-output refusal")
			}
		})
	}
}

type addressRecordingRunner struct {
	command   execx.Command
	result    execx.Result
	responses []execx.Result
	calls     int
	deadline  bool
	err       error
}

func (r *addressRecordingRunner) Run(ctx context.Context, command execx.Command) (execx.Result, error) {
	r.command, r.calls = command, r.calls+1
	_, r.deadline = ctx.Deadline()
	if r.err != nil {
		return execx.Result{}, r.err
	}
	if len(r.responses) >= r.calls {
		return r.responses[r.calls-1], nil
	}
	return r.result, nil
}
