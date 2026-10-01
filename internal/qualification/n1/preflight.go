package n1

import (
	"context"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/clock"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/contract"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/fixed"
)

type preflightDependencies struct {
	now                       func() (clock.Reading, error)
	static                    func() (fixed.StaticInputs, error)
	doctor, inventory, census func(context.Context, fixed.StaticInputs) error
}

// Preflight has only observation dependencies. No cleanup guard or dispatch
// callback can enter this graph. All elapsed since the original stamp charges
// both caps conservatively, including authentication before this process exists.
func executePreflight(ctx context.Context, w contract.Window, d preflightDependencies) error {
	if !w.Valid() || d.now == nil || d.static == nil || d.doctor == nil || d.inventory == nil || d.census == nil {
		return ErrRefused
	}
	g := clock.Guard{Window: w}
	check := func() error {
		r, e := d.now()
		if e != nil || ctx.Err() != nil || g.Check(r) != nil || r.Wall-w.StartedUnixNS >= contract.AttendanceNS || r.Continuous-w.ContinuousStartNS >= contract.AttendanceNS || r.Wall-w.StartedUnixNS >= contract.ActiveNS || r.Continuous-w.ContinuousStartNS >= contract.ActiveNS {
			return ErrRefused
		}
		return nil
	}
	if check() != nil {
		return ErrRefused
	}
	s, e := d.static()
	if e != nil || s.LockSHA != w.LockSHA || !s.Lock.Valid() || !s.Protected.Valid() || !s.Build.Valid() || !s.Catalogue.Valid() || check() != nil {
		return ErrRefused
	}
	for _, observe := range []func(context.Context, fixed.StaticInputs) error{d.doctor, d.inventory, d.census} {
		if check() != nil || observe(ctx, s) != nil || check() != nil {
			return ErrRefused
		}
	}
	return check()
}
func stampWindow(r clock.Reading, id, sha string) (contract.Window, error) {
	if r.Wall == 0 || r.Continuous == 0 || r.Wall > 9223372036854775807-contract.WindowNS || r.Continuous > ^uint64(0)-contract.WindowNS {
		return contract.Window{}, ErrRefused
	}
	w := contract.Window{LockSHA: sha, ID: id, StartedUnixNS: r.Wall, ExpiresUnixNS: r.Wall + contract.WindowNS, ContinuousStartNS: r.Continuous, ContinuousLimitNS: r.Continuous + contract.WindowNS}
	if !w.Valid() {
		return contract.Window{}, ErrRefused
	}
	return w, nil
}
