package tart

import (
	"context"
	"errors"
	"testing"

	"github.com/weshofmann/boxwarden/internal/backend"
	"github.com/weshofmann/boxwarden/internal/execx"
)

const stoppedList = `[
  {"State":"stopped","Accessed":"2026-09-01T06:51:16Z","Name":"boxwarden-work-dev","Source":"local","Disk":30,"Running":false,"Size":10}
]`

func TestParseListReturnsMatchingStoppedObject(t *testing.T) {
	got, err := parseList([]byte(stoppedList), "boxwarden-work-dev")
	if err != nil {
		t.Fatalf("parseList() error = %v", err)
	}
	want := backend.Observation{ObjectID: "boxwarden-work-dev", Exists: true, State: backend.ObjectStopped}
	if got != want {
		t.Fatalf("parseList() = %#v, want %#v", got, want)
	}
}

func TestParseListReportsAnAbsentObject(t *testing.T) {
	got, err := parseList([]byte(`[]`), "boxwarden-work-missing")
	if err != nil {
		t.Fatalf("parseList() error = %v", err)
	}
	if got.Exists {
		t.Fatal("parseList().Exists = true, want false")
	}
	if got.State != backend.ObjectUnknown {
		t.Fatalf("parseList().State = %q, want %q", got.State, backend.ObjectUnknown)
	}
}

func TestParseListRejectsAmbiguousOrUnexpectedTartData(t *testing.T) {
	for name, raw := range map[string]string{
		"malformed JSON":           `{`,
		"unknown object field":     `[ {"State":"stopped","Accessed":"2026-09-01T06:51:16Z","Name":"boxwarden-work-dev","Source":"local","Disk":30,"Running":false,"Size":10,"Extra":true} ]`,
		"missing state":            `[ {"Accessed":"2026-09-01T06:51:16Z","Name":"boxwarden-work-dev","Source":"local","Disk":30,"Running":false,"Size":10} ]`,
		"mismatched running state": `[ {"State":"running","Accessed":"2026-09-01T06:51:16Z","Name":"boxwarden-work-dev","Source":"local","Disk":30,"Running":false,"Size":10} ]`,
		"unrecognized state":       `[ {"State":"paused","Accessed":"2026-09-01T06:51:16Z","Name":"boxwarden-work-dev","Source":"local","Disk":30,"Running":false,"Size":10} ]`,
		"duplicate object":         `[{"State":"stopped","Accessed":"2026-09-01T06:51:16Z","Name":"boxwarden-work-dev","Source":"local","Disk":30,"Running":false,"Size":10},{"State":"stopped","Accessed":"2026-09-01T06:51:16Z","Name":"boxwarden-work-dev","Source":"local","Disk":30,"Running":false,"Size":10}]`,
		"wrong number type":        `[ {"State":"stopped","Accessed":"2026-09-01T06:51:16Z","Name":"boxwarden-work-dev","Source":"local","Disk":"30","Running":false,"Size":10} ]`,
		"incomplete other object":  `[ {"State":"stopped","Accessed":"2026-09-01T06:51:16Z","Name":"boxwarden-work-dev","Source":"local","Disk":30,"Running":false,"Size":10}, {"Name":"unrelated"} ]`,
		"missing source":           `[ {"State":"stopped","Accessed":"2026-09-01T06:51:16Z","Name":"boxwarden-work-dev","Disk":30,"Running":false,"Size":10} ]`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parseList([]byte(raw), "boxwarden-work-dev"); err == nil {
				t.Fatal("parseList() error = nil, want rejection")
			}
		})
	}
}

func TestObserverUsesOnlyTartListJSON(t *testing.T) {
	runner := &recordingRunner{result: execx.Result{Stdout: stoppedList}}
	observer := New(runner, "/opt/homebrew/bin/tart")

	got, err := observer.Observe(context.Background(), "boxwarden-work-dev")
	if err != nil {
		t.Fatalf("Observe() error = %v", err)
	}
	if !got.Exists || got.State != backend.ObjectStopped {
		t.Fatalf("Observe() = %#v, want stopped existing observation", got)
	}
	if got, want := runner.command.Path, "/opt/homebrew/bin/tart"; got != want {
		t.Fatalf("command path = %q, want %q", got, want)
	}
	if got, want := runner.command.Args, []string{"list", "--format", "json"}; !sameStrings(got, want) {
		t.Fatalf("command args = %#v, want %#v", got, want)
	}
}

func TestObserverRejectsTruncatedOutputAndCommandFailure(t *testing.T) {
	for name, runner := range map[string]*recordingRunner{
		"truncated output": {result: execx.Result{Truncated: true}},
		"command failure":  {err: errors.New("tart unavailable")},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := New(runner, "/opt/homebrew/bin/tart").Observe(context.Background(), "boxwarden-work-dev"); err == nil {
				t.Fatal("Observe() error = nil, want actionable observation error")
			}
		})
	}
}

type recordingRunner struct {
	command execx.Command
	result  execx.Result
	err     error
}

func (r *recordingRunner) Run(_ context.Context, command execx.Command) (execx.Result, error) {
	r.command = command
	return r.result, r.err
}

func sameStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for index := range got {
		if got[index] != want[index] {
			return false
		}
	}
	return true
}
