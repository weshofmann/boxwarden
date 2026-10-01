//go:build n1clipboarddiagnostic && !n1candidate

package worker

import (
	"context"
	"errors"

	"github.com/weshofmann/boxwarden/internal/lock"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/contract"
	"github.com/weshofmann/boxwarden/internal/sshx"
)

func (w *Worker) withLocks(ctx context.Context, action func() error) (err error) {
	if w == nil || action == nil {
		return ErrRefused
	}
	transition, e := lock.TryAcquire(ctx, w.domain.StateRoot, "transition-n1qualification-"+roleName)
	if e != nil {
		return ErrRefused
	}
	defer func() { err = errors.Join(err, transition.Release()) }()
	session, e := lock.TryAcquire(ctx, w.domain.StateRoot, "session-n1qualification-"+roleName)
	if e != nil {
		return ErrRefused
	}
	defer func() { err = errors.Join(err, session.Release()) }()
	return action()
}

const genericName = "artifacts/guest-bootstrap-generic"

func (w *Worker) bundle() (sshx.N1GuestArtifacts, sshx.N1GuestHashes, error) {
	h := sshx.N1GuestSourceHashes()
	var a sshx.N1GuestArtifacts
	genericSHA := ""
	for _, f := range w.static.Lock.Files {
		if f.Name == genericName {
			if genericSHA != "" {
				return a, h, ErrRefused
			}
			genericSHA = f.SHA
		}
	}
	if genericSHA != h.GenericHelper || len(w.static.Lock.Files) < 20 {
		return a, h, ErrRefused
	}
	h.TrialHelper = w.static.Lock.Files[6].SHA
	h.Overlay = w.static.Lock.Files[7].SHA
	if w.static.Lock.Files[8].SHA != h.Production || w.static.Lock.Files[9].SHA != h.Observer || w.static.Lock.Files[16].SHA != h.Stager || w.static.Lock.Files[17].SHA != h.Inspector || w.static.Lock.Files[18].SHA != h.Controls || w.static.Lock.Files[19].SHA != h.Connect {
		return a, h, ErrRefused
	}
	paths := []string{contract.PackageRoot + "/" + genericName, contract.StaticFilePath(6), contract.StaticFilePath(7), contract.StaticFilePath(8), contract.StaticFilePath(9)}
	hashes := []string{h.GenericHelper, h.TrialHelper, h.Overlay, h.Production, h.Observer}
	dest := []*sshx.N1GuestArtifact{&a.GenericHelper, &a.TrialHelper, &a.Overlay, &a.Production, &a.Observer}
	for i, p := range paths {
		limit := 1 << 20
		if i < 2 {
			limit = 16 << 20
		}
		raw, _, e := readLeaf(p, 0500, limit)
		if e != nil || contract.SHA(raw) != hashes[i] {
			return sshx.N1GuestArtifacts{}, h, ErrRefused
		}
		*dest[i] = sshx.N1GuestArtifact{Bytes: raw, ExpectedSHA256: hashes[i]}
	}
	return a, h, nil
}
func (w *Worker) Stage(ctx context.Context, id Identity) (result sshx.N1StageResult, err error) {
	err = w.withLocks(ctx, func() error {
		o, e := w.Inspect(ctx, id)
		if e != nil {
			return e
		}
		a, h, e := w.bundle()
		if e != nil {
			return e
		}
		result, e = w.guest.StageN1Guest(ctx, o.Connection, guestBinding(id), a, h)
		return e
	})
	return result, err
}
func (w *Worker) InspectGuest(ctx context.Context, id Identity, phase sshx.N1GuestPhase) (result sshx.N1InspectResult, err error) {
	err = w.withLocks(ctx, func() error {
		o, e := w.Inspect(ctx, id)
		if e != nil {
			return e
		}
		a, h, e := w.bundle()
		if e != nil {
			return e
		}
		result, e = w.guest.InspectN1Guest(ctx, o.Connection, sshx.N1InspectRequest{Binding: guestBinding(id), Artifacts: a, Hashes: h, Phase: phase})
		return e
	})
	return result, err
}
