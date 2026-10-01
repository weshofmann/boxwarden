package coordinator

import (
	"github.com/weshofmann/boxwarden/internal/qualification/n1/clock"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/contract"
)

// All time before C is conservatively charged to both pools. Within C each
// measured interval is classified once; regressions permanently close dispatch.
type budget struct {
	guard              clock.Guard
	last               clock.Reading
	active, attendance uint64
	failed             bool
}

func newBudget(w contract.Window, r clock.Reading) (*budget, error) {
	b := &budget{guard: clock.Guard{Window: w}, last: r}
	if b.guard.Check(r) != nil {
		return nil, ErrRefused
	}
	n := r.Wall - w.StartedUnixNS
	if c := r.Continuous - w.ContinuousStartNS; c > n {
		n = c
	}
	b.active = n
	b.attendance = n
	if n >= contract.ActiveNS || n >= contract.AttendanceNS {
		return nil, ErrRefused
	}
	return b, nil
}
func (b *budget) check(r clock.Reading, attendance bool) error {
	if b == nil || b.failed || b.guard.Check(r) != nil {
		if b != nil {
			b.failed = true
		}
		return ErrRefused
	}
	n := r.Wall - b.last.Wall
	if c := r.Continuous - b.last.Continuous; c > n {
		n = c
	}
	b.last = r
	if attendance {
		if n > contract.AttendanceNS-b.attendance {
			b.failed = true
			return ErrRefused
		}
		b.attendance += n
	} else {
		if n > contract.ActiveNS-b.active {
			b.failed = true
			return ErrRefused
		}
		b.active += n
	}
	if b.active >= contract.ActiveNS || b.attendance >= contract.AttendanceNS {
		b.failed = true
		return ErrRefused
	}
	return nil
}
func (b *budget) receipt() contract.Budget {
	return contract.Budget{ClosedUnixNS: b.last.Wall, ClosedContinuousNS: b.last.Continuous, ActiveSpentNS: b.active, AttendanceSpentNS: b.attendance}
}
