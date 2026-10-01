//go:build n1diagnostic && n1clipboarddiagnostic && !n1candidate

package coordinator

import (
	"context"
	"github.com/weshofmann/boxwarden/internal/config"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/contract"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/worker"
	"github.com/weshofmann/boxwarden/internal/session"
)

// A failed window never resumes. Before dispatch expires, a once-only public
// containment Stop may act on a created identity whose current Running
// generation can be freshly observed. Failure never authorizes delete/H/F.
func (e *engine) contain() {
	if e == nil || e.closed || e.budget.failed {
		return
	}
	for i := 0; i < 2; i++ {
		f := e.fresh[i]
		if !f.Created || !contract.UUID(f.Record.ID) {
			continue
		}
		used := false
		for _, a := range e.ledger.attempts {
			if a.Command == role(i)+"-stop" {
				used = true
			}
		}
		if used {
			continue
		}
		record, err := session.LoadRecord(contract.StateRoot, config.N1Domain, roleName(i))
		if err != nil || record.ID != f.Record.ID || record.Backend.ObjectID != f.Record.Backend.ObjectID || record.IntendedState != session.StateRunning || !contract.UUID(record.StartGeneration) {
			continue
		}
		e.pair[i].Identity = worker.Identity{Session: record.ID, Backend: record.Backend.ObjectID, Generation: record.StartGeneration}
		_ = e.phase(role(i)+"-stop", false, func(ctx context.Context, id string) (any, int, error) {
			err := e.stop(ctx, i)
			return struct {
				Containment bool `json:"containment"`
				Reaped      bool `json:"reaped"`
			}{true, err == nil}, 3, err
		})
		if e.budget.failed {
			return
		}
	}
}
