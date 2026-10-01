//go:build n1diagnostic && n1clipboarddiagnostic && !n1candidate

package coordinator

import (
	"context"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/clock"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/contract"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/fixed"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/worker"
	"github.com/weshofmann/boxwarden/internal/session"
	"io"
	"os"
	"time"
)

type engine struct {
	window     contract.Window
	static     fixed.StaticInputs
	budget     *budget
	ledger     *ledger
	archive    *archive
	now        func() (clock.Reading, error)
	closed     bool
	localDirty bool
	commands   []commandWitness
	pair       [2]worker.RuntimeObservation
	fresh      [2]session.FreshCreation
}
type phaseReceipt struct {
	Version     int              `json:"version"`
	WindowID    string           `json:"window_id"`
	Attempt     contract.Attempt `json:"attempt"`
	Status      string           `json:"status"`
	Subcommands int              `json:"subcommands"`
	Envelope    phaseEnvelope    `json:"envelope"`
	Commands    []commandWitness `json:"commands"`
	Data        any              `json:"data"`
}

func (e *engine) check(attendance bool) error {
	r, err := e.now()
	if err != nil || e.budget.check(r, attendance) != nil {
		return ErrRefused
	}
	return nil
}
func (e *engine) deadline(attendance bool) time.Time {
	remaining := contract.ActiveNS - e.budget.active
	if attendance {
		remaining = contract.AttendanceNS - e.budget.attendance
	}
	w := e.window.ExpiresUnixNS
	if e.budget.last.Wall+remaining < w {
		w = e.budget.last.Wall + remaining
	}
	return time.Unix(0, int64(w))
}

// A phase is one immutable command ID and a statically bounded group of fixed
// subcommands. Reservation is durable before any effect. Unknown is terminal.
func (e *engine) phase(name string, attendance bool, fn func(context.Context, string) (any, int, error)) error {
	if e.closed || e.check(false) != nil {
		return ErrRefused
	}
	a, err := e.ledger.reserve(name)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithDeadline(context.Background(), e.deadline(attendance))
	defer cancel()
	start := e.budget.last
	e.commands = nil
	data, count, err := fn(ctx, a.ID)
	ce := e.check(attendance)
	status := "complete"
	if err != nil || ce != nil {
		status = "unknown"
	}
	if count < 0 || count > 8 {
		return ErrRefused
	}
	if e.ledger.closeAttempt(a.ID) != nil {
		return ErrRefused
	}
	a.Closed = true
	raw, re := contract.Encode(phaseReceipt{1, e.window.ID, a, status, count, phaseEnvelope{start, e.budget.last}, append([]commandWitness{}, e.commands...), data})
	if re != nil || e.archive.write("receipt-"+name+".json", raw) != nil {
		return ErrRefused
	}
	if ce != nil {
		return ce
	}
	return err
}
func Run() error {
	if os.Getuid() != 501 || os.Geteuid() != 501 || len(os.Args) != 4 || os.Args[1] != "--run" || os.Args[2] != "--approved-lock-sha" {
		return ErrRefused
	}
	s, err := fixed.ReadStatic()
	if err != nil || os.Args[3] != s.LockSHA {
		return ErrRefused
	}
	raw, err := io.ReadAll(io.LimitReader(os.Stdin, contract.MaxWitnessBytes+1))
	if err != nil {
		return ErrRefused
	}
	w, err := contract.ParseOrigin(raw)
	if err != nil || w.LockSHA != s.LockSHA {
		return ErrRefused
	}
	now, err := clock.Now()
	if err != nil {
		return ErrRefused
	}
	b, err := newBudget(w, now)
	if err != nil {
		return err
	}
	// Absent fixed roots are consumed once. Any partial prior window refuses.
	for _, p := range []string{contract.EvidenceRoot, contract.StateRoot, contract.EnrolledTarget(0), contract.EnrolledTarget(1)} {
		if _, e := os.Lstat(p); !os.IsNotExist(e) {
			return ErrRefused
		}
	}
	if mkdirPrivate(contract.EvidenceRoot) != nil || fixed.CheckEvidenceDirectory() != nil {
		return ErrRefused
	}
	l, err := newLedger(contract.EvidenceRoot+"/one-use", w)
	if err != nil {
		return err
	}
	a, err := newArchive(contract.EvidenceRoot+"/"+contract.ArchiveDirectory, w)
	if err != nil {
		return err
	}
	e := &engine{window: w, static: s, budget: b, ledger: l, archive: a, now: clock.Now}
	origin, _ := contract.Encode(w)
	if a.write("package-origin.json", origin) != nil {
		return ErrRefused
	}
	return e.execute()
}
func (e *engine) execute() (err error) {
	defer func() {
		if err != nil {
			e.contain()
			e.closed = true
			if !e.ledger.closed {
				_ = e.ledger.closeDispatch()
			}
		}
	}()
	if e.phase("enroll", false, func(ctx context.Context, id string) (any, int, error) {
		err := enroll(e.static)
		return struct {
			Configs [2]string `json:"configs"`
		}{[2]string{contract.StockConfigSHA, contract.CandidateConfigSHA}}, 2, err
	}) != nil {
		return ErrRefused
	}
	if e.phase("domain-init", false, func(ctx context.Context, id string) (any, int, error) {
		r, err := e.cli(ctx, 0, "domain", "init")
		if err == nil && string(r.raw) != "domain: n1qualification\nmanagement-ca: initialized\n" {
			err = ErrRefused
		}
		return struct {
			Initialized bool `json:"initialized"`
		}{err == nil}, 1, err
	}) != nil {
		return ErrRefused
	}
	if e.phase("install", true, func(ctx context.Context, id string) (any, int, error) {
		if e.executable(1) != nil {
			return nil, 0, ErrRefused
		}
		e.witness(contract.SudoPath, []string{"--", contract.StaticFilePath(1), "internal", "host-install"})
		r, err := install(ctx, e.static)
		return r, 1, err
	}) != nil {
		return ErrRefused
	}
	for i, name := range []string{"control", "candidate"} {
		if e.phase(name+"-create", false, func(ctx context.Context, id string) (any, int, error) {
			var c session.FreshCreation
			err := e.worker(ctx, i, "create", worker.Identity{}, nil, &c)
			if err == nil && (!c.Created || c.Record.Domain != "n1qualification" || string(c.Record.Name) != roleName(i) || c.Record.IntendedState != session.StateStopped || c.Record.Mode != session.ModeQuarantine || c.Record.GoldenRevision != contract.BaseName || c.Record.RecipeIntentDigest != "" || c.Record.Backend.Kind != "tart" || !contract.UUID(c.Record.ID)) {
				err = ErrRefused
			}
			e.fresh[i] = c
			return struct {
				Created bool   `json:"created"`
				Session string `json:"session"`
				Backend string `json:"backend"`
			}{c.Created, c.Record.ID, c.Record.Backend.ObjectID}, 1, err
		}) != nil {
			return ErrRefused
		}
		if e.phase(name+"-start", false, func(ctx context.Context, id string) (any, int, error) {
			err := e.start(ctx, i)
			return struct {
				Ready bool `json:"ready"`
			}{err == nil}, 3, err
		}) != nil {
			return ErrRefused
		}
	}
	if !distinct(e.pair) {
		return ErrRefused
	}
	if e.review("initial") != nil {
		return ErrRefused
	}
	for i, name := range []string{"control", "candidate"} {
		if e.phase(name+"-stage", false, func(ctx context.Context, id string) (any, int, error) {
			var r any
			var v stageReceipt
			err := e.worker(ctx, i, "stage", e.pair[i].Identity, nil, &v)
			if err == nil && (v.Version != 1 || v.Code != "staged" || v.Installed != 9 || v.Binding.SessionID != e.pair[i].Identity.Session || v.Binding.Generation != e.pair[i].Identity.Generation || v.Binding.BackendObject != e.pair[i].Identity.Backend) {
				err = ErrRefused
			}
			r = v
			return r, 1, err
		}) != nil {
			return ErrRefused
		}
		if e.phase(name+"-restart", false, func(ctx context.Context, id string) (any, int, error) {
			old := e.pair[i]
			err := e.stop(ctx, i)
			if err == nil {
				err = e.start(ctx, i)
			}
			if err == nil && (e.pair[i].Identity.Generation == old.Identity.Generation || e.pair[i].Certificate == old.Certificate || e.pair[i].Identity.Session != old.Identity.Session || e.pair[i].Identity.Backend != old.Identity.Backend) {
				err = ErrRefused
			}
			if err == nil {
				err = e.guestFinal(ctx, i)
			}
			return struct {
				NewGeneration bool `json:"new_generation"`
			}{err == nil}, 6, err
		}) != nil {
			return ErrRefused
		}
	}
	if !distinct(e.pair) || e.review("final") != nil {
		return ErrRefused
	}
	// Negative clipboard/transport results are retained, then network proceeds
	// only if actual same-generation public readiness survives its own admission.
	if err := e.clipboard(); err != nil {
		return ErrRefused
	}
	if err := e.network(); err != nil {
		return ErrRefused
	}
	// Stop dispatch is still planned here. There is no new work after this finite
	// teardown, and an uncertain stop prevents delete and cleanup handoff.
	for i, name := range []string{"control", "candidate"} {
		if e.phase(name+"-stop", false, func(ctx context.Context, id string) (any, int, error) {
			err := e.stop(ctx, i)
			return struct {
				Reaped bool `json:"reaped"`
			}{err == nil}, 3, err
		}) != nil {
			return ErrRefused
		}
	}
	for i, name := range []string{"control", "candidate"} {
		if e.localDirty {
			return ErrRefused
		}
		if e.phase(name+"-delete", false, func(ctx context.Context, id string) (any, int, error) {
			err := e.delete(ctx, i)
			return struct {
				Deleted bool `json:"deleted"`
			}{err == nil}, 2, err
		}) != nil {
			return ErrRefused
		}
	}
	// Archive reservation precedes permanent dispatch closure. Finalization does
	// not invoke a worker or reopen dispatch, and handoff is outside the archive.
	if e.phase("archive", false, func(ctx context.Context, id string) (any, int, error) {
		return struct {
			RuntimeClean bool `json:"runtime_clean"`
		}{true}, 0, nil
	}) != nil {
		return ErrRefused
	}
	e.closed = true
	if e.ledger.closeDispatch() != nil {
		return ErrRefused
	}
	if e.localDirty {
		return ErrRefused
	}
	if e.check(false) != nil {
		return ErrRefused
	}
	sha, files, n, err := e.archive.finalize(true, true)
	if err != nil {
		return err
	}
	h := contract.Handoff{Version: 1, Window: e.window, Configs: [2]string{contract.StockConfigSHA, contract.CandidateConfigSHA}, Attempts: e.ledger.attempts, ArchiveSHA: sha, ArchiveFiles: files, ArchiveBytes: n, DispatchClosed: true, RuntimeClean: true}
	for i, p := range e.pair {
		h.Pair[i] = contract.Peer{HostPinSHA: p.HostPinSHA, Role: role(i), Session: p.Identity.Session, Backend: p.Identity.Backend, Generation: p.Identity.Generation, Reaped: true, Deleted: true}
		pin, err := privateRead(contract.StateRoot+"/identity/ssh-host-pins/"+p.Identity.Session+".json", 4096)
		if err != nil {
			return err
		}
		if _, err = contract.ParseHostPin(pin, h.Pair[i]); err != nil {
			return err
		}
	}
	for i, name := range contract.PublicRecordNames {
		raw, err := privateRead(contract.StateRoot+"/"+name, 4096)
		if err != nil {
			return err
		}
		if i == 0 {
			if _, err = contract.ParseCAPublic(raw); err != nil || contract.SHA(raw) != e.pair[0].CASHA {
				return ErrRefused
			}
		} else {
			if _, err = contract.ParseBase(raw); err != nil {
				return ErrRefused
			}
		}
		h.PublicRecords[i] = contract.PublicRecord{Name: name, SHA: contract.SHA(raw), WindowID: e.window.ID}
	}
	if e.check(false) != nil {
		return ErrRefused
	}
	h.Budget = e.budget.receipt()
	if !h.Valid() {
		return ErrRefused
	}
	raw, err := contract.Encode(h)
	if err != nil || exclusive(contract.EvidenceRoot, contract.HandoffName, raw) != nil || e.check(false) != nil {
		return ErrRefused
	}
	return nil
}
