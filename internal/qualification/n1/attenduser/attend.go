package attenduser

import (
	"github.com/weshofmann/boxwarden/internal/qualification/n1/clock"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/contract"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/fixed"
	"os"
	"os/user"
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

// Terminal entry is an inherited unnamed pipe from actual L exec, not a file
// marker or caller-selected argument. Its witness supplies no process authority.
func RunUser() error {
	if len(os.Args) != 1 || os.Getuid() != 501 || os.Geteuid() != 501 {
		return fixed.ErrRefused
	}
	u, e := user.LookupId("501")
	if e != nil || u.Username != "devel" || u.HomeDir != "/Users/devel" {
		return fixed.ErrRefused
	}
	group, e := user.LookupGroupId("501")
	if e != nil || group.Name != "boxwarden-operators" {
		return fixed.ErrRefused
	}
	groups, e := os.Getgroups()
	if e != nil {
		return fixed.ErrRefused
	}
	member := false
	for _, gid := range groups {
		if gid == 501 {
			member = true
		}
	}
	if !member {
		return fixed.ErrRefused
	}
	v, e := fixed.ReadInputs()
	if e != nil {
		return fixed.ErrRefused
	}
	g := clock.New(v.Handoff)
	if check(&g, v) != nil {
		return fixed.ErrRefused
	}
	w, e := fixed.ReadTransition(time.Unix(0, int64(v.Handoff.DeadlineWall())))
	if e != nil || w.LockSHA != v.LockSHA || w.HandoffSHA != v.HandoffSHA || w.WindowID != v.Handoff.Window.ID {
		return fixed.ErrRefused
	}
	if check(&g, v) != nil || fixed.CheckArtifact(2, v.Lock) != nil {
		return fixed.ErrRefused
	}
	if fixed.CheckArtifact(3, v.Lock) != nil || fixed.CheckSudo() != nil {
		return fixed.ErrRefused
	}
	if check(&g, v) != nil {
		return fixed.ErrRefused
	}
	child, e := fixed.WaitChild("/usr/bin/sudo", []string{"--", contract.ArtifactPath(3)}, fixed.Environment(false), os.Stdin, time.Unix(0, int64(v.Handoff.DeadlineWall())))
	if e != nil {
		return fixed.ErrRefused
	}
	if _, e = fixed.ValidateChild(v, child, 3); e != nil {
		return fixed.ErrRefused
	}
	if check(&g, v) != nil {
		return fixed.ErrRefused
	}
	if fixed.CheckArtifact(5, v.Lock) != nil {
		return fixed.ErrRefused
	}
	if check(&g, v) != nil {
		return fixed.ErrRefused
	}
	child, e = fixed.WaitChild(contract.ArtifactPath(5), nil, fixed.Environment(false), nil, time.Unix(0, int64(v.Handoff.DeadlineWall())))
	return finalize(&g, child, e, clock.Now)
}

// F owns its final integrity and planned retirement. After retained F success,
// require only original sticky clock/budget; retired inputs cannot be reread.
func finalize(g *clock.Guard, child fixed.ChildResult, waitErr error, now func() (clock.Reading, error)) error {
	if waitErr != nil || child.Exit != 0 || !child.Closed || len(child.Raw) != 0 {
		return fixed.ErrRefused
	}
	r, e := now()
	if e != nil || g.Check(r) != nil {
		return fixed.ErrRefused
	}
	return nil
}
