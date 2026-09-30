//go:build !n1diagnostic || n1candidate

package sessionruntime

import (
	"errors"
	"github.com/weshofmann/boxwarden/internal/session"
	"github.com/weshofmann/boxwarden/internal/supervisor"
	"testing"
)

func checkDiagnosticStarterForTest(*testing.T, *session.Service, error) bool  { return false }
func diagnosticEnrollmentEnforcedForTest() bool                               { return false }
func configureDiagnosticProcessOwnerForTest(*Owner, supervisor.LaunchRequest) {}

func TestOrdinaryUnclaimedDiskClosePreservesPublishedJoin(t *testing.T) {
	original, cleanup := errors.New("start refused"), errors.New("unclaimed close failed")
	o := &Owner{}
	got := o.finishUnclaimedDiagnosticDisks(original, cleanup)
	if !errors.Is(got, original) || !errors.Is(got, cleanup) || got.Error() != errors.Join(original, cleanup).Error() {
		t.Fatal("ordinary unclaimed close lost a published error cause")
	}
	if got = o.finishUnclaimedDiagnosticDisks(nil, cleanup); !errors.Is(got, cleanup) {
		t.Fatal("ordinary unclaimed close changed published successful-start cleanup result")
	}
	if got = o.finishUnclaimedDiagnosticDisks(original, nil); !errors.Is(got, original) {
		t.Fatal("ordinary unclaimed close lost original refusal")
	}
}
