package clock

import (
	"errors"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/contract"
)

var ErrClock = errors.New("n1 clock refused")

type Reading struct{ Wall, Continuous uint64 }
type Guard struct {
	Window  contract.Window
	last    Reading
	seen    bool
	failed  bool
	handoff *contract.Handoff
}

func (g *Guard) Check(r Reading) error {
	if g.failed || g.handoff != nil && g.handoff.Check(r.Wall, r.Continuous) != nil || g.Window.Check(r.Wall, r.Continuous) != nil || g.seen && (r.Wall < g.last.Wall || r.Continuous < g.last.Continuous) {
		g.failed = true
		return ErrClock
	}
	g.last = r
	g.seen = true
	return nil
}

func New(h contract.Handoff) Guard {
	if _, _, e := h.Deadlines(); e != nil {
		return Guard{Window: h.Window, failed: true}
	}
	return Guard{Window: h.Window, handoff: &h}
}
