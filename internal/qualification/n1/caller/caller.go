package caller

import (
	"bytes"
	"io"
	"os"
	"os/user"
	"time"

	"github.com/weshofmann/boxwarden/internal/qualification/n1/clock"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/contract"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/fixed"
)

func preflightRemaining(w contract.Window, wall, continuous uint64) error {
	if w.Check(wall, continuous) != nil || wall-w.StartedUnixNS >= contract.AttendanceNS || continuous-w.ContinuousStartNS >= contract.AttendanceNS || wall-w.StartedUnixNS >= contract.ActiveNS || continuous-w.ContinuousStartNS >= contract.ActiveNS {
		return fixed.ErrRefused
	}
	return nil
}
func operator() error {
	if os.Getuid() != 501 || os.Geteuid() != 501 {
		return fixed.ErrRefused
	}
	u, e := user.LookupId("501")
	if e != nil || u.Username != "devel" || u.HomeDir != "/Users/devel" {
		return fixed.ErrRefused
	}
	g, e := user.LookupGroupId("501")
	if e != nil || g.Name != "boxwarden-operators" {
		return fixed.ErrRefused
	}
	gs, e := os.Getgroups()
	if e != nil {
		return fixed.ErrRefused
	}
	for _, v := range gs {
		if v == 501 {
			return nil
		}
	}
	return fixed.ErrRefused
}

// Run is the fixed attended entry. It never stamps a new origin or authorizes
// itself. The literal approval SHA must agree with the owner's static lock.
type callerOps struct {
	authorize             func() error
	static                func() (fixed.StaticInputs, error)
	inputs                func() (fixed.Inputs, error)
	now                   func() (clock.Reading, error)
	artifact              func(int, contract.StaticLock) error
	coordinator, attendee string
}

func Run() error {
	return run(os.Args, os.Stdin, callerOps{operator, fixed.ReadStatic, fixed.ReadInputs, clock.Now, fixed.CheckArtifact, contract.ArtifactPath(1), contract.ArtifactPath(2)})
}

// The private seam changes readers in focused tests; all production paths and
// effect boundaries remain compile-fixed in Run and real Wait/pipe/exec here.
func run(args []string, stdin io.Reader, ops callerOps) error {
	if ops.authorize == nil || ops.static == nil || ops.inputs == nil || ops.now == nil || ops.artifact == nil || ops.coordinator == "" || ops.attendee == "" || len(args) != 4 || args[1] != "--run" || args[2] != "--approved-lock-sha" || ops.authorize() != nil {
		return fixed.ErrRefused
	}
	s, e := ops.static()
	if e != nil || args[3] != s.LockSHA {
		return fixed.ErrRefused
	}
	raw, e := io.ReadAll(io.LimitReader(stdin, contract.MaxWitnessBytes+1))
	if e != nil || len(raw) > contract.MaxWitnessBytes {
		return fixed.ErrRefused
	}
	origin, e := contract.ParseOrigin(raw)
	if e != nil || origin.LockSHA != s.LockSHA {
		return fixed.ErrRefused
	}
	guard := clock.Guard{Window: origin}
	r, e := ops.now()
	if e != nil || guard.Check(r) != nil || preflightRemaining(origin, r.Wall, r.Continuous) != nil || ops.artifact(0, s.Lock) != nil || ops.artifact(1, s.Lock) != nil {
		return fixed.ErrRefused
	}
	r, e = ops.now()
	if e != nil || guard.Check(r) != nil || preflightRemaining(origin, r.Wall, r.Continuous) != nil {
		return fixed.ErrRefused
	}
	child, e := waitCoordinator(ops.coordinator, []string{"--run", "--approved-lock-sha", s.LockSHA}, fixed.Environment(false), bytes.NewReader(raw), time.Unix(0, int64(origin.ExpiresUnixNS)), func(f *os.File) error { return f.Close() })
	if e != nil || child.Exit != contract.PlannedExit || !child.Closed {
		return fixed.ErrRefused
	}
	// Files are supplemental; they are consumed only after retained C returned.
	v, e := ops.inputs()
	if e != nil || v.LockSHA != s.LockSHA || v.Handoff.Window != origin {
		return fixed.ErrRefused
	}
	post := clock.New(v.Handoff)
	r, e = ops.now()
	if e != nil || guard.Check(r) != nil || post.Check(r) != nil || ops.artifact(2, v.Lock) != nil {
		return fixed.ErrRefused
	}
	witness, e := contract.EncodeWitness(contract.Witness{Version: 1, Phase: 1, LockSHA: v.LockSHA, WindowID: origin.ID, HandoffSHA: v.HandoffSHA, Exit: child.Exit})
	if e != nil {
		return fixed.ErrRefused
	}
	read, write, e := os.Pipe()
	if e != nil {
		return fixed.ErrRefused
	}
	n, we := write.Write(witness)
	ce := write.Close()
	if we != nil || ce != nil || n != len(witness) {
		read.Close()
		return fixed.ErrRefused
	}
	r, e = ops.now()
	if e != nil || guard.Check(r) != nil || post.Check(r) != nil || ops.artifact(2, v.Lock) != nil {
		read.Close()
		return fixed.ErrRefused
	}
	return replaceImage(ops.attendee, []string{ops.attendee}, fixed.Environment(false), read)
}
