package lifecycle

import (
	"strings"
	"testing"

	"github.com/weshofmann/boxwarden/internal/backend"
	"github.com/weshofmann/boxwarden/internal/session"
)

func TestReconcileRecognizesStableConsistentState(t *testing.T) {
	got := Reconcile(session.StateStopped, backend.Observation{
		ObjectID: "boxwarden-work-dev",
		Exists:   true,
		State:    backend.ObjectStopped,
	})
	if got.Consistency != Consistent {
		t.Fatalf("Consistency = %q, want %q", got.Consistency, Consistent)
	}
	if got.Diagnostic != "" {
		t.Fatalf("Diagnostic = %q, want empty", got.Diagnostic)
	}
}

func TestReconcileMarksRunningBackendWithoutSupervisorReadinessAsDrift(t *testing.T) {
	got := Reconcile(session.StateRunning, backend.Observation{
		ObjectID: "boxwarden-work-dev",
		Exists:   true,
		State:    backend.ObjectRunning,
	})
	if got.Consistency != Drift {
		t.Fatalf("Consistency = %q, want %q", got.Consistency, Drift)
	}
	for _, want := range []string{"supervisor ownership/readiness", "non-ready", "not adopted"} {
		if !strings.Contains(got.Diagnostic, want) {
			t.Errorf("Diagnostic = %q, want guidance containing %q", got.Diagnostic, want)
		}
	}
}

func TestReconcileMarksIntentAndBackendStateDrift(t *testing.T) {
	for name, input := range map[string]struct {
		intended session.IntendedState
		observed backend.Observation
	}{
		"running record but stopped VM": {
			intended: session.StateRunning,
			observed: backend.Observation{ObjectID: "boxwarden-work-dev", Exists: true, State: backend.ObjectStopped},
		},
		"missing VM": {
			intended: session.StateRunning,
			observed: backend.Observation{ObjectID: "boxwarden-work-dev", State: backend.ObjectUnknown},
		},
	} {
		t.Run(name, func(t *testing.T) {
			got := Reconcile(input.intended, input.observed)
			if got.Consistency != Drift {
				t.Fatalf("Consistency = %q, want %q", got.Consistency, Drift)
			}
			if got.Diagnostic == "" {
				t.Fatal("Diagnostic is empty, want repair guidance")
			}
		})
	}
}

func TestReconcileLeavesAmbiguousStateVisible(t *testing.T) {
	got := Reconcile(session.StateStarting, backend.Observation{
		ObjectID: "boxwarden-work-dev",
		Exists:   true,
		State:    backend.ObjectRunning,
	})
	if got.Consistency != Indeterminate {
		t.Fatalf("Consistency = %q, want %q", got.Consistency, Indeterminate)
	}
	if got.Diagnostic == "" {
		t.Fatal("Diagnostic is empty, want lifecycle explanation")
	}
}

func TestReconcileClassifiesCreatingAndMissingAsRetryableTransition(t *testing.T) {
	got := Reconcile(session.StateCreating, backend.Observation{
		ObjectID: "boxwarden-work-dev",
		State:    backend.ObjectUnknown,
	})
	if got.Consistency != Indeterminate {
		t.Fatalf("Consistency = %q, want %q", got.Consistency, Indeterminate)
	}
	if got.Diagnostic == "" {
		t.Fatal("Diagnostic is empty, want create-reconciliation guidance")
	}
}

func TestReconcileRejectsInvalidObservationState(t *testing.T) {
	got := Reconcile(session.StateStopped, backend.Observation{
		ObjectID: "boxwarden-work-dev",
		Exists:   true,
		State:    backend.ObjectState("paused"),
	})
	if got.Consistency != Indeterminate {
		t.Fatalf("Consistency = %q, want %q", got.Consistency, Indeterminate)
	}
}
