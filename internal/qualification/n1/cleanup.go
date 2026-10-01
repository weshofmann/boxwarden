package n1

import (
	"context"
	"errors"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/contract"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/fixed"
)

type cleanupGuard interface {
	Revalidate(context.Context) error
	RemoveExact(context.Context) error
	Close() error
}
type stateGuard interface {
	Revalidate(context.Context) error
	Close() error
}
type cleanupDependencies struct {
	acquire                func(context.Context) (cleanupGuard, error)
	inventory              func(context.Context) (stateGuard, error)
	storage, census, check func(context.Context) error
	publish                func(contract.Completion) error
}

func executeCleanup(ctx context.Context, v fixed.Inputs, d cleanupDependencies) error {

	if ctx.Err() != nil {
		return ErrRefused
	}
	g, e := d.acquire(ctx)
	if e != nil {
		return ErrRefused
	}
	state, e := d.inventory(ctx)
	if e != nil {
		return errors.Join(ErrRefused, g.Close())
	}
	closeAll := func() error { return errors.Join(state.Close(), g.Close()) }
	refuse := func() error { return errors.Join(ErrRefused, closeAll()) }
	for _, check := range []func(context.Context) error{d.check, d.storage, d.census, state.Revalidate, g.Revalidate, d.check, d.storage, d.census, state.Revalidate, g.Revalidate, d.check} {
		if check(ctx) != nil {
			return refuse()
		}
	}
	if g.RemoveExact(ctx) != nil {
		return refuse()
	}
	if closeAll() != nil {
		return ErrRefused
	}
	if d.check(ctx) != nil {
		return ErrRefused
	}
	c := contract.Completion{Version: 1, Window: v.Handoff.Window, HandoffSHA: v.HandoffSHA, ArchiveSHA: v.Handoff.ArchiveSHA, SoftnetSHA: contract.SoftnetSHA, Removed: 3, DirectoryRemoved: true, ParentSynced: true, HandlesClosed: true}
	if d.publish(c) != nil {
		return ErrRefused
	}
	return nil
}
