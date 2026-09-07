package tart

import (
	"context"
	"testing"

	"github.com/weshofmann/boxwarden/internal/backend"
	"github.com/weshofmann/boxwarden/internal/execx"
)

// Catch observation against a PATH-selected executable, default Tart home,
// or inherited credentials instead of the admitted child namespace.
func TestQualifiedObserverUsesExactExecutableHomeAndClosedEnvironment(t *testing.T) {
	t.Setenv("PROVIDER_TOKEN", "must-not-inherit")
	runner := &recordingRunner{result: execx.Result{Stdout: stoppedList}}
	observer := NewQualifiedObserver(runner, "/qualified/tart", "/operator/tart-home")
	got, err := observer.Observe(context.Background(), "boxwarden-work-dev")
	if err != nil || got.ObjectID != "boxwarden-work-dev" || got.State != backend.ObjectStopped {
		t.Fatalf("Observe = %#v, %v", got, err)
	}
	if runner.command.Path != "/qualified/tart" || !sameStrings(runner.command.Args, []string{"list", "--format", "json"}) {
		t.Fatalf("wrong invocation: %#v", runner.command)
	}
	wantEnv := []string{"PATH=/usr/bin:/bin", "TART_HOME=/operator/tart-home", "LANG=C", "LC_ALL=C"}
	if !sameStrings(runner.command.Env, wantEnv) {
		t.Fatalf("observation environment = %#v, want closed %#v", runner.command.Env, wantEnv)
	}
}

func TestQualifiedObserverRejectsInvalidPathsBeforeExecution(t *testing.T) {
	for _, paths := range [][2]string{{"tart", "/private/tart"}, {"/qualified/tart", "relative"}, {"/qualified/tart", "/"}, {"/qualified/tart", "/a/../b"}} {
		runner := &recordingRunner{result: execx.Result{Stdout: stoppedList}}
		if _, err := NewQualifiedObserver(runner, paths[0], paths[1]).Observe(context.Background(), "boxwarden-work-dev"); err == nil {
			t.Fatalf("accepted invalid qualified paths %#v", paths)
		}
		if runner.command.Path != "" {
			t.Fatal("executed before path admission")
		}
	}
}
