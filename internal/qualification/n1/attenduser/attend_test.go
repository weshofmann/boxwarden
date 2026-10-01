package attenduser

import (
	"errors"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/clock"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/contract"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/fixed"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func finalGuard() clock.Guard {
	w := contract.Window{LockSHA: strings.Repeat("a", 64), ID: "11111111-1111-1111-1111-111111111111", StartedUnixNS: 10, ExpiresUnixNS: 10 + contract.WindowNS, ContinuousStartNS: 20, ContinuousLimitNS: 20 + contract.WindowNS}
	return clock.New(contract.Handoff{Window: w, Budget: contract.Budget{ClosedUnixNS: 10, ClosedContinuousNS: 20}})
}
func TestFinalizationAfterPlannedInputRetirement(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"stock.enrolled.json", "candidate.enrolled.json"} {
		p := filepath.Join(dir, name)
		if e := os.WriteFile(p, []byte("private fixture"), 0600); e != nil {
			t.Fatal(e)
		}
		if e := os.Remove(p); e != nil {
			t.Fatal(e)
		}
	}
	for _, name := range []string{"stock.enrolled.json", "candidate.enrolled.json"} {
		if _, e := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(e) {
			t.Fatal("planned fixture retirement missing", e)
		}
	}
	g := finalGuard()
	e := finalize(&g, fixed.ChildResult{Exit: 0, Closed: true}, nil, func() (clock.Reading, error) { return clock.Reading{Wall: 11, Continuous: 21}, nil })
	if e != nil {
		t.Fatal("successful actual-result seam refused planned config disappearance", e)
	}
}
func TestFinalizationRefusesLostResultsAndClock(t *testing.T) {
	for _, mode := range []string{"nonzero", "lostEOF", "close", "output", "wait", "expiry", "regression", "clock-error", "sticky"} {
		t.Run(mode, func(t *testing.T) {
			g := finalGuard()
			r := fixed.ChildResult{Exit: 0, Closed: true}
			var waitErr, errorClock error
			now := clock.Reading{Wall: 12, Continuous: 22}
			switch mode {
			case "nonzero":
				r.Exit = 7
			case "lostEOF", "close":
				r.Closed = false
			case "output":
				r.Raw = []byte("unexpected")
			case "wait":
				waitErr = errors.New("wait")
			case "expiry":
				now.Wall = 10 + contract.AttendanceNS
			case "regression":
				if g.Check(clock.Reading{Wall: 13, Continuous: 23}) != nil {
					t.Fatal("initial")
				}
			case "clock-error":
				errorClock = errors.New("clock")
			case "sticky":
				g.Check(clock.Reading{Wall: 10 + contract.AttendanceNS, Continuous: 22})
			}
			if finalize(&g, r, waitErr, func() (clock.Reading, error) { return now, errorClock }) == nil {
				t.Fatal("terminal result admitted", mode)
			}
		})
	}
}
