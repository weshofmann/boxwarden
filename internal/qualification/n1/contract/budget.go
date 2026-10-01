package contract

const ActiveNS uint64 = 20 * 60 * 1000000000
const AttendanceNS uint64 = 10 * 60 * 1000000000

type Budget struct {
	ClosedUnixNS       uint64 `json:"closed_unix_ns"`
	ClosedContinuousNS uint64 `json:"closed_continuous_ns"`
	ActiveSpentNS      uint64 `json:"active_spent_ns"`
	AttendanceSpentNS  uint64 `json:"attendance_spent_ns"`
}

func (b Budget) Valid(w Window) bool {
	if !w.Valid() || b.ActiveSpentNS > ActiveNS || b.AttendanceSpentNS > AttendanceNS || b.ClosedUnixNS < w.StartedUnixNS || b.ClosedUnixNS >= w.ExpiresUnixNS || b.ClosedContinuousNS < w.ContinuousStartNS || b.ClosedContinuousNS >= w.ContinuousLimitNS {
		return false
	}
	spent := b.ActiveSpentNS + b.AttendanceSpentNS
	return spent >= b.ClosedUnixNS-w.StartedUnixNS && spent >= b.ClosedContinuousNS-w.ContinuousStartNS
}

// All elapsed after this original closed handoff conservatively consumes BOTH
// remaining budgets. No actor can classify time anew or reset either limit.
func (h Handoff) Deadlines() (uint64, uint64, error) {
	b, w := h.Budget, h.Window
	if !b.Valid(w) {
		return 0, 0, ErrRefused
	}
	remain := ActiveNS - b.ActiveSpentNS
	if a := AttendanceNS - b.AttendanceSpentNS; a < remain {
		remain = a
	}
	wall, continuous := w.ExpiresUnixNS, w.ContinuousLimitNS
	if remain < wall-b.ClosedUnixNS {
		wall = b.ClosedUnixNS + remain
	}
	if remain < continuous-b.ClosedContinuousNS {
		continuous = b.ClosedContinuousNS + remain
	}
	return wall, continuous, nil
}
func (h Handoff) DeadlineWall() uint64 {
	wall, _, e := h.Deadlines()
	if e != nil {
		return 0
	}
	return wall
}
func (h Handoff) Check(wall, continuous uint64) error {
	a, b, e := h.Deadlines()
	if e != nil || h.Window.Check(wall, continuous) != nil || wall < h.Budget.ClosedUnixNS || continuous < h.Budget.ClosedContinuousNS || wall >= a || continuous >= b {
		return ErrRefused
	}
	return nil
}
