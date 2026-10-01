package closeout

import (
	"context"
	"errors"
	"time"

	"github.com/weshofmann/boxwarden/internal/qualification/n1/clock"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/contract"
)

type flowInput struct {
	Handoff             contract.Handoff
	LockSHA, HandoffSHA string
}
type retirable interface {
	Revalidate(context.Context) error
	Retire(context.Context) error
	Close() error
}
type flowDependencies struct {
	load       func() (flowInput, error)
	transition func(time.Time) (contract.Witness, error)
	completion func() (contract.Completion, string, error)
	now        func() (clock.Reading, error)
	static     func() error
	archive    func(context.Context, contract.Handoff) error
	reserve    func(context.Context, contract.Witness, contract.Handoff) error
	absence    func(context.Context) error
	doctor     func(context.Context) error
	protected  func(context.Context) error
	inventory  func(context.Context, contract.Handoff, func() error) (retirable, error)
	configs    func(context.Context, func() error) error
	finish     func(context.Context, contract.Witness, contract.Handoff) error
}

// Private test seam. The native entry below constructs only compile-pinned
// operations. No receipt, caller, argument or environment selects a callback.
func runFlow(ctx context.Context, d flowDependencies) (err error) {
	if ctx.Err() != nil || d.load == nil || d.transition == nil || d.completion == nil || d.now == nil || d.static == nil || d.archive == nil || d.reserve == nil || d.absence == nil || d.doctor == nil || d.protected == nil || d.inventory == nil || d.configs == nil || d.finish == nil {
		return ErrRefused
	}
	v, e := d.load()
	if e != nil || !v.Handoff.Valid() || v.Handoff.Window.LockSHA != v.LockSHA {
		return ErrRefused
	}
	h := v.Handoff
	g := clock.New(h)
	clockCheck := func() error {
		r, e := d.now()
		if e != nil || ctx.Err() != nil || g.Check(r) != nil {
			return ErrRefused
		}
		return nil
	}
	gate := func() error {
		if clockCheck() != nil {
			return ErrRefused
		}
		return nil
	}
	if gate() != nil || d.static() != nil || gate() != nil {
		return ErrRefused
	}
	w, e := d.transition(time.Unix(0, int64(h.DeadlineWall())))
	if e != nil || !w.Valid() || w.Phase != 3 || w.LockSHA != v.LockSHA || w.HandoffSHA != v.HandoffSHA || w.WindowID != h.Window.ID || gate() != nil {
		return ErrRefused
	}
	c, sha, e := d.completion()
	r, re := d.now()
	if e != nil || re != nil || sha != w.CompletionSHA || contract.ValidateCompletion(c, h, v.HandoffSHA, 0, true, r.Wall, r.Continuous) != nil || g.Check(r) != nil {
		return ErrRefused
	}
	if gate() != nil {
		return ErrRefused
	}
	// O_EXCL intent is the one-use reservation. Even a later read-only refusal
	// leaves it intact, so this source never resumes a failed/unknown closeout.
	if d.reserve(ctx, w, h) != nil || gate() != nil {
		return ErrRefused
	}
	if d.archive(ctx, h) != nil || gate() != nil {
		return ErrRefused
	}
	for _, observe := range []func(context.Context) error{d.absence, d.protected, d.doctor, d.protected} {
		if gate() != nil || observe(ctx) != nil || gate() != nil {
			return ErrRefused
		}
	}
	state, e := d.inventory(ctx, h, clockCheck)
	defer func() {
		if state != nil {
			err = errors.Join(err, state.Close())
		}
	}()
	if e != nil || state == nil || gate() != nil || state.Revalidate(ctx) != nil || gate() != nil {
		return ErrRefused
	}
	if d.archive(ctx, h) != nil || gate() != nil || d.absence(ctx) != nil || gate() != nil || d.static() != nil || gate() != nil {
		return ErrRefused
	}
	if state.Retire(ctx) != nil {
		return ErrRefused
	}
	ce := state.Close()
	state = nil
	if ce != nil || gate() != nil || d.configs(ctx, clockCheck) != nil || gate() != nil {
		return ErrRefused
	}
	for _, observe := range []func(context.Context) error{d.absence, d.doctor, d.protected} {
		if gate() != nil || observe(ctx) != nil || gate() != nil {
			return ErrRefused
		}
	}
	if d.archive(ctx, h) != nil || gate() != nil || d.static() != nil || gate() != nil || d.finish(ctx, w, h) != nil || gate() != nil {
		return ErrRefused
	}
	return nil
}
