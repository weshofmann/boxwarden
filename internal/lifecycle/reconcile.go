// Package lifecycle owns reconciliation between persisted lifecycle intent and
// read-only backend observation.
package lifecycle

import (
	"fmt"

	"github.com/weshofmann/boxwarden/internal/backend"
	"github.com/weshofmann/boxwarden/internal/session"
)

type Consistency string

const (
	Consistent    Consistency = "consistent"
	Drift         Consistency = "drift"
	Indeterminate Consistency = "indeterminate"
)

type Reconciliation struct {
	Consistency Consistency
	Diagnostic  string
}

// Reconcile classifies a session record against a backend observation. It is
// intentionally pure: status inspection never changes backend or local state.
func Reconcile(intended session.IntendedState, observed backend.Observation) Reconciliation {
	expected, stable := expectedObservedState(intended)
	if !stable {
		return Reconciliation{
			Consistency: Indeterminate,
			Diagnostic:  fmt.Sprintf("persisted lifecycle intent %q is transitional and requires lifecycle reconciliation", intended),
		}
	}
	if !observed.Exists {
		return Reconciliation{
			Consistency: Drift,
			Diagnostic:  "the backend object is missing; inspect the session before attempting repair",
		}
	}
	if observed.State != backend.ObjectRunning && observed.State != backend.ObjectStopped {
		return Reconciliation{
			Consistency: Indeterminate,
			Diagnostic:  fmt.Sprintf("the backend reported unsupported observed state %q", observed.State),
		}
	}
	if intended == session.StateRunning && observed.State == backend.ObjectRunning {
		return Reconciliation{
			Consistency: Drift,
			Diagnostic:  "the running backend is non-ready because supervisor ownership/readiness is not verified and is not adopted",
		}
	}

	if observed.State != expected {
		return Reconciliation{
			Consistency: Drift,
			Diagnostic:  fmt.Sprintf("persisted intent is %q but the backend observes %q", intended, observed.State),
		}
	}
	return Reconciliation{Consistency: Consistent}
}

func expectedObservedState(intended session.IntendedState) (backend.ObjectState, bool) {
	switch intended {
	case session.StateRunning:
		return backend.ObjectRunning, true
	case session.StateStopped:
		return backend.ObjectStopped, true
	default:
		return backend.ObjectUnknown, false
	}
}
