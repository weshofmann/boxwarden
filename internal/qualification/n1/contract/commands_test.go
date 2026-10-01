package contract

import (
	"fmt"
	"testing"
)

func TestFiniteCoordinatorCommandPhasesAreOnceOnly(t *testing.T) {
	names := []string{"enroll", "domain-init", "install", "control-create", "candidate-create", "control-start", "candidate-start", "control-stage", "candidate-stage", "control-restart", "candidate-restart", "initial-review", "final-review", "control-copy", "control-read", "candidate-copy", "candidate-read", "network-controls-before", "network-controls-after", "positive-before", "positive-after", "observer-control", "observer-candidate", "arm", "connect", "collect", "control-stop", "candidate-stop", "control-delete", "candidate-delete", "archive"}
	h := fixture()
	h.Attempts = nil
	for i, n := range names {
		h.Attempts = append(h.Attempts, Attempt{ID: fmt.Sprintf("%08x-1111-1111-1111-111111111111", i+1), Command: n, Closed: true})
	}
	if !h.Valid() {
		t.Fatal("finite31 phase ledger refused")
	}
	for _, n := range []string{"network-controls", "restart", "arbitrary", "archive-again"} {
		if commandID(n) {
			t.Fatal("unreviewed phase admitted", n)
		}
	}
	h.Attempts = append(h.Attempts, Attempt{ID: "aaaaaaaa-1111-1111-1111-111111111111", Command: "archive", Closed: true})
	if h.Valid() {
		t.Fatal("duplicate phase reopened")
	}
}
