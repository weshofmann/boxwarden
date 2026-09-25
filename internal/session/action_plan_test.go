package session

import (
	"errors"
	"reflect"
	"testing"

	"github.com/weshofmann/boxwarden/internal/domain"
	"github.com/weshofmann/boxwarden/internal/recipe"
)

func automaticPlanFixture(t *testing.T) (Record, recipe.Recipe, ActionAttempt) {
	t.Helper()
	value := recipe.Recipe{Version: 1,
		Source:  recipe.Source{Kind: "ubuntu-24.04.4-desktop-arm64", SHA256: "c2610520bf582976839a1724c669e1cfed0547427be5a0ad12d457b92b46ffbe"},
		Machine: recipe.Machine{CPUs: 4, MemoryMiB: 4096, SystemDiskGiB: 30},
		Steps: []recipe.Step{
			{ID: "startup-a", Phase: "startup", Argv: []string{"/usr/bin/true"}},
			{ID: "once-a", Phase: "once", Argv: []string{"/usr/bin/true"}},
			{ID: "once-b", Phase: "once", Argv: []string{"/usr/bin/true"}},
			{ID: "manual", Phase: "reconfigure", Argv: []string{"/usr/bin/true"}},
		},
	}
	_, digest, err := recipe.CanonicalIntent(value)
	if err != nil {
		t.Fatal(err)
	}
	record := Record{Version: recordVersion, Domain: domain.ID("alpha"), Name: Name("dev"),
		ID: "13b0bf73-3bd5-4f1c-8bdc-71d50c36d6d0", Mode: ModeClean,
		IntendedState: StateRunning, Backend: BackendRef{Kind: "tart", ObjectID: "boxwarden-alpha-dev"},
		GoldenRevision: "golden-r1", RecipeIntentDigest: digest,
		StartGeneration: "00000000-0000-4000-8000-000000000003",
		Readiness:       ReadinessRecord{Status: ReadinessReady}}
	attempt := ActionAttempt{Version: 1, Domain: record.Domain, SessionName: string(record.Name),
		SessionID: record.ID, BackendObject: record.Backend.ObjectID, Generation: record.StartGeneration,
		RecipeDigest: digest, ActionID: "once-a", ActionPhase: "once",
		AttemptID: "00112233-4455-4677-8899-aabbccddeeff", State: ActionAttemptSucceeded,
		ReceiptSHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	return record, value, attempt
}

func TestPlanAutomaticActionsOrdersPhasesAndResumesCompletedSteps(t *testing.T) {
	record, value, once := automaticPlanFixture(t)
	if got, err := PlanAutomaticActions(record, value, nil); err != nil ||
		!reflect.DeepEqual(got, []recipe.Step{value.Steps[1], value.Steps[2], value.Steps[0]}) {
		t.Fatalf("fresh plan = %+v, %v", got, err)
	}
	if got, err := PlanAutomaticActions(record, value, []ActionAttempt{once}); err != nil ||
		!reflect.DeepEqual(got, []recipe.Step{value.Steps[2], value.Steps[0]}) {
		t.Fatalf("resumed plan = %+v, %v", got, err)
	}
	olderSystem := once
	olderSystem.BackendObject = "boxwarden-alpha-old"
	if got, err := PlanAutomaticActions(record, value, []ActionAttempt{olderSystem}); err != nil ||
		!reflect.DeepEqual(got, []recipe.Step{value.Steps[1], value.Steps[2], value.Steps[0]}) {
		t.Fatalf("new system inherited once result = %+v, %v", got, err)
	}
}

func TestPlanAutomaticActionsBlocksUnresolvedAttemptsAcrossStartGenerations(t *testing.T) {
	record, value, once := automaticPlanFixture(t)
	uncertain := once
	uncertain.State = ActionAttemptIndeterminate
	uncertain.ReceiptSHA256 = ""
	if got, err := PlanAutomaticActions(record, value, []ActionAttempt{uncertain}); got != nil || !errors.Is(err, ErrAutomaticActionUnresolved) {
		t.Fatalf("indeterminate once was replayed: %+v, %v", got, err)
	}
	startup := uncertain
	startup.ActionID, startup.ActionPhase = "startup-a", "startup"
	startup.Generation = "00000000-0000-4000-8000-000000000002"
	if got, err := PlanAutomaticActions(record, value, []ActionAttempt{startup}); got != nil || !errors.Is(err, ErrAutomaticActionUnresolved) {
		t.Fatalf("older indeterminate startup was replayed: %+v, %v", got, err)
	}
	startup.State = ActionAttemptSkipped
	if got, err := PlanAutomaticActions(record, value, []ActionAttempt{startup}); err != nil ||
		!reflect.DeepEqual(got, []recipe.Step{value.Steps[1], value.Steps[2], value.Steps[0]}) {
		t.Fatalf("old skipped startup prevented new generation: %+v, %v", got, err)
	}
	manual := uncertain
	manual.ActionID, manual.ActionPhase = "manual", "reconfigure"
	if got, err := PlanAutomaticActions(record, value, []ActionAttempt{manual}); got != nil || !errors.Is(err, ErrAutomaticActionUnresolved) {
		t.Fatalf("unresolved explicit reconfigure was ignored before automatic steps: %+v, %v", got, err)
	}
}

func TestPlanAutomaticActionsRejectsCompletedStepAfterMissingPredecessor(t *testing.T) {
	record, value, attempt := automaticPlanFixture(t)
	attempt.ActionID = "once-b"
	if got, err := PlanAutomaticActions(record, value, []ActionAttempt{attempt}); err == nil || got != nil {
		t.Fatalf("later once step bypassed missing predecessor: %+v, %v", got, err)
	}
	attempt.ActionID, attempt.ActionPhase = "startup-a", "startup"
	if got, err := PlanAutomaticActions(record, value, []ActionAttempt{attempt}); err == nil || got != nil {
		t.Fatalf("startup completion bypassed once phase: %+v, %v", got, err)
	}
}

func TestPlanAutomaticActionsRejectsInvalidSessionBinding(t *testing.T) {
	record, value, _ := automaticPlanFixture(t)
	record.Domain = ""
	if got, err := PlanAutomaticActions(record, value, nil); err == nil || got != nil {
		t.Fatalf("invalid domain received automatic steps: %+v, %v", got, err)
	}
}
