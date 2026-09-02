package tart

import (
	"context"
	"errors"
	"testing"

	"github.com/weshofmann/boxwarden/internal/backend"
	"github.com/weshofmann/boxwarden/internal/execx"
)

func TestCreatorUsesOnlyExactCloneAndRandomMACArgv(t *testing.T) {
	runner := &creatorRecordingRunner{}
	creator := New(runner, "/opt/homebrew/bin/tart")

	if err := creator.Clone(context.Background(), "golden-work", "boxwarden-work-dev"); err != nil {
		t.Fatalf("Clone() error = %v", err)
	}
	if err := creator.RandomizeMAC(context.Background(), "boxwarden-work-dev"); err != nil {
		t.Fatalf("RandomizeMAC() error = %v", err)
	}

	want := []execx.Command{
		{Path: "/opt/homebrew/bin/tart", Args: []string{"clone", "golden-work", "boxwarden-work-dev"}},
		{Path: "/opt/homebrew/bin/tart", Args: []string{"set", "boxwarden-work-dev", "--random-mac"}},
	}
	if !sameCommands(runner.commands, want) {
		t.Fatalf("commands = %#v, want %#v", runner.commands, want)
	}
}

func TestCreatorAddsDeadlinesWhenCallerProvidesNone(t *testing.T) {
	runner := &creatorRecordingRunner{}
	creator := New(runner, "/opt/homebrew/bin/tart")
	if err := creator.Clone(context.Background(), "golden-work", "boxwarden-work-dev"); err != nil {
		t.Fatalf("Clone() error = %v", err)
	}
	if err := creator.RandomizeMAC(context.Background(), "boxwarden-work-dev"); err != nil {
		t.Fatalf("RandomizeMAC() error = %v", err)
	}
	if got := runner.hasDeadlines; len(got) != 2 || !got[0] || !got[1] {
		t.Fatalf("runner context deadlines = %#v, want bounded clone and MAC commands", got)
	}
}

func TestCreatorRejectsUnsafeObjectIDsBeforeExecution(t *testing.T) {
	for name, operation := range map[string]func(backend.Creator) error{
		"clone option source": func(creator backend.Creator) error {
			return creator.Clone(context.Background(), "--source", "boxwarden-work-dev")
		},
		"clone path target": func(creator backend.Creator) error {
			return creator.Clone(context.Background(), "golden-work", "../boxwarden-work-dev")
		},
		"random MAC option": func(creator backend.Creator) error {
			return creator.RandomizeMAC(context.Background(), "--boxwarden-work-dev")
		},
		"random MAC control": func(creator backend.Creator) error {
			return creator.RandomizeMAC(context.Background(), "boxwarden-work\x00dev")
		},
	} {
		t.Run(name, func(t *testing.T) {
			runner := &creatorRecordingRunner{}
			if err := operation(New(runner, "/opt/homebrew/bin/tart")); err == nil {
				t.Fatal("creator operation error = nil, want invalid object ID rejection")
			}
			if len(runner.commands) != 0 {
				t.Fatalf("commands = %#v, want no execution", runner.commands)
			}
		})
	}
}

func TestCreatorRejectsMissingRunnerOrExecutable(t *testing.T) {
	for name, creator := range map[string]backend.Creator{
		"missing runner":     New(nil, "/opt/homebrew/bin/tart"),
		"missing executable": New(&creatorRecordingRunner{}, " "),
	} {
		t.Run(name, func(t *testing.T) {
			if err := creator.Clone(context.Background(), "golden-work", "boxwarden-work-dev"); err == nil {
				t.Fatal("Clone() error = nil, want setup rejection")
			}
		})
	}
}

func TestCreatorPropagatesRunnerDeadlineAndRejectsTruncatedOutput(t *testing.T) {
	deadline := context.DeadlineExceeded
	for name, runner := range map[string]*creatorRecordingRunner{
		"runner error":      {err: errors.New("Tart unavailable")},
		"runner deadline":   {err: deadline},
		"truncated success": {result: execx.Result{Truncated: true}},
	} {
		t.Run(name, func(t *testing.T) {
			err := New(runner, "/opt/homebrew/bin/tart").Clone(context.Background(), "golden-work", "boxwarden-work-dev")
			if err == nil {
				t.Fatal("Clone() error = nil, want actionable failure")
			}
			if name == "runner deadline" && !errors.Is(err, deadline) {
				t.Fatalf("Clone() error = %v, want wrapped %v", err, deadline)
			}
			if got, want := runner.commands, []execx.Command{{Path: "/opt/homebrew/bin/tart", Args: []string{"clone", "golden-work", "boxwarden-work-dev"}}}; !sameCommands(got, want) {
				t.Fatalf("commands = %#v, want %#v", got, want)
			}
		})
	}
}

type creatorRecordingRunner struct {
	commands     []execx.Command
	hasDeadlines []bool
	result       execx.Result
	err          error
}

func (r *creatorRecordingRunner) Run(ctx context.Context, command execx.Command) (execx.Result, error) {
	r.commands = append(r.commands, command)
	_, hasDeadline := ctx.Deadline()
	r.hasDeadlines = append(r.hasDeadlines, hasDeadline)
	return r.result, r.err
}

func sameCommands(got, want []execx.Command) bool {
	if len(got) != len(want) {
		return false
	}
	for index := range got {
		if got[index].Path != want[index].Path || !sameStrings(got[index].Args, want[index].Args) || got[index].Env != nil {
			return false
		}
	}
	return true
}
