package clock

import (
	"github.com/weshofmann/boxwarden/internal/qualification/n1/contract"
	"strings"
	"testing"
)

func TestRegressionAndExpiryCannotRecover(t *testing.T) {
	w := contract.Window{LockSHA: strings.Repeat("a", 64), ID: "11111111-1111-1111-1111-111111111111", StartedUnixNS: 10, ExpiresUnixNS: 10 + contract.WindowNS, ContinuousStartNS: 20, ContinuousLimitNS: 20 + contract.WindowNS}
	for _, bad := range []Reading{{10, 19}, {9, 20}, {10 + contract.WindowNS, 20}, {10, 20 + contract.WindowNS}} {
		g := Guard{Window: w}
		if g.Check(Reading{11, 21}) != nil {
			t.Fatal("valid clock refused")
		}
		if g.Check(bad) == nil {
			t.Fatal("bad clock accepted")
		}
		if g.Check(Reading{12, 22}) == nil {
			t.Fatal("invalid clock recovered")
		}
	}
}

func TestCumulativeAttendanceExpiryIsSticky(t *testing.T) {
	const minute = 60 * 1000000000
	w := contract.Window{LockSHA: strings.Repeat("a", 64), ID: "11111111-1111-1111-1111-111111111111", StartedUnixNS: 10, ExpiresUnixNS: 10 + contract.WindowNS, ContinuousStartNS: 20, ContinuousLimitNS: 20 + contract.WindowNS}
	h := contract.Handoff{Window: w, Budget: contract.Budget{ClosedUnixNS: 10 + 19*minute, ClosedContinuousNS: 20 + 19*minute, ActiveSpentNS: 10 * minute, AttendanceSpentNS: 9 * minute}}
	g := New(h)
	if g.Check(Reading{h.Budget.ClosedUnixNS, h.Budget.ClosedContinuousNS}) != nil {
		t.Fatal("fresh handoff refused")
	}
	if g.Check(Reading{h.Budget.ClosedUnixNS + 2*minute, h.Budget.ClosedContinuousNS + 2*minute}) == nil {
		t.Fatal("prior attendance reset")
	}
	if g.Check(Reading{h.Budget.ClosedUnixNS + 1, h.Budget.ClosedContinuousNS + 1}) == nil {
		t.Fatal("expired actor recovered")
	}
}
