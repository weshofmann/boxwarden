package tart

import (
	"context"
	"testing"
)

func TestDeleteUsesOneValidatedObjectID(t *testing.T) {
	runner := &lifecycleRecordingRunner{}
	deleter := NewDeleter(runner, "/opt/qualified/tart", "/Users/wes/.boxwarden/tart")
	if err := deleter.Delete(context.Background(), "boxwarden-work-dev"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if got, want := runner.command.Path, "/opt/qualified/tart"; got != want {
		t.Fatalf("command path = %q, want %q", got, want)
	}
	if got, want := runner.command.Args, []string{"delete", "boxwarden-work-dev"}; !sameLifecycleStrings(got, want) {
		t.Fatalf("command args = %#v, want %#v", got, want)
	}
	if got, want := runner.command.Env, []string{"TART_HOME=/Users/wes/.boxwarden/tart", "LANG=C", "LC_ALL=C"}; !sameLifecycleStrings(got, want) {
		t.Fatalf("command environment = %#v, want %#v", got, want)
	}
	if err := deleter.Delete(context.Background(), "--all"); err == nil {
		t.Fatal("Delete() unsafe object ID error = nil, want refusal")
	}
}
