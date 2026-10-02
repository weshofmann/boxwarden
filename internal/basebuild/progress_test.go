package basebuild

import (
	"context"
	"reflect"
	"testing"
)

func TestBuildProgressFollowsDurableStages(t *testing.T) {
	usePortableACLFixture(t)
	in := exampleInputs(t)
	events := []string{}
	phases := []Phase{}
	deps := Dependencies{Checks: &fakeChecks{}, Seed: fakeSeed{events: &events}, VM: &fakeVM{events: &events, run: &fakeRun{events: &events}}, Progress: func(p Phase) {
		state, err := ReadAttempt(in.AttemptRoot + "/" + in.AttemptID)
		if err != nil || state.Phase != p {
			t.Fatalf("progress before durable stage: %s %+v %v", p, state, err)
		}
		phases = append(phases, p)
	}}
	if _, err := Build(context.Background(), in, deps); err != nil {
		t.Fatal(err)
	}
	want := []Phase{PhaseReserved, PhaseRendered, PhaseCreated, PhaseRunning, PhasePreparing, PhaseFinalizing, PhaseStopping, PhaseCandidateStopped}
	if !reflect.DeepEqual(phases, want) {
		t.Fatalf("progress %v, want %v", phases, want)
	}
}
