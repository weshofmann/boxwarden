//go:build n1clipboarddiagnostic && !n1candidate

package worker

import (
	"context"

	"github.com/weshofmann/boxwarden/internal/backend"
	"github.com/weshofmann/boxwarden/internal/golden"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/contract"
	"github.com/weshofmann/boxwarden/internal/session"
)

// Only the compile-fixed role is creatable. A partial reservation consumes it;
// a later call cannot adopt that state as a newly created qualification guest.
func (w *Worker) Create(ctx context.Context) (session.FreshCreation, error) {
	if w == nil || w.observer == nil || w.creator == nil {
		return session.FreshCreation{}, ErrRefused
	}
	if _, e := golden.RegisterRevision(ctx, w.domain, contract.BaseName, w.observer); e != nil {
		return session.FreshCreation{}, ErrRefused
	}
	c, e := session.NewService(w.domain, w.observer, w.creator).CreateFreshFromRevision(ctx, roleName, session.ModeQuarantine, contract.BaseName)
	if e != nil || !c.Created || c.Record.Mode != session.ModeQuarantine || c.Record.GoldenRevision != contract.BaseName || string(c.Record.Name) != roleName || c.Record.IntendedState != session.StateStopped || c.Record.Backend.Kind != "tart" || c.Record.RecipeIntentDigest != "" {
		return session.FreshCreation{}, ErrRefused
	}
	o, e := w.observer.Observe(ctx, c.Record.Backend.ObjectID)
	if e != nil || o.ObjectID != c.Record.Backend.ObjectID || !o.Exists || o.State != backend.ObjectStopped {
		return session.FreshCreation{}, ErrRefused
	}
	return c, nil
}
