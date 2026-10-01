package attendroot

import (
	"github.com/weshofmann/boxwarden/internal/qualification/n1/clock"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/contract"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/fixed"
	"os"
	"time"
)

func check(g *clock.Guard, v fixed.Inputs) error {
	r, e := clock.Now()
	if e != nil || g.Check(r) != nil {
		return fixed.ErrRefused
	}
	current, e := fixed.ReadInputs()
	if e != nil || current.LockSHA != v.LockSHA || current.HandoffSHA != v.HandoffSHA {
		return fixed.ErrRefused
	}
	return nil
}

func RunRoot() error {
	if len(os.Args) != 1 || os.Getuid() != 0 || os.Geteuid() != 0 {
		return fixed.ErrRefused
	}
	v, e := fixed.ReadInputs()
	if e != nil {
		return fixed.ErrRefused
	}
	g := clock.New(v.Handoff)
	if check(&g, v) != nil || fixed.CheckArtifact(3, v.Lock) != nil {
		return fixed.ErrRefused
	}
	if fixed.CheckArtifact(4, v.Lock) != nil {
		return fixed.ErrRefused
	}
	if check(&g, v) != nil {
		return fixed.ErrRefused
	}
	child, e := fixed.WaitChild(contract.ArtifactPath(4), nil, fixed.Environment(true), nil, time.Unix(0, int64(v.Handoff.DeadlineWall())))
	if e != nil {
		return fixed.ErrRefused
	}
	w, e := fixed.ValidateChild(v, child, 2)
	if e != nil || check(&g, v) != nil {
		return fixed.ErrRefused
	}
	w.Phase = 3
	return fixed.WriteWitness(w)
}
