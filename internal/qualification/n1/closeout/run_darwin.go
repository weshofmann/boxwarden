//go:build darwin && cgo && n1diagnostic && n1clipboarddiagnostic && !n1candidate

package closeout

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/weshofmann/boxwarden/internal/qualification/n1/clock"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/contract"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/fixed"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/pathmeta"
)

// Run is compiled only for the exact unprivileged F actor. Arguments, stdin,
// environment and receipts cannot supply removal paths or dispatch selectors.
func Run() error {
	if len(os.Args) != 1 || os.Getuid() != 501 || os.Geteuid() != 501 {
		return ErrRefused
	}
	var v fixed.Inputs
	var guard clock.Guard
	tick := func() error {
		r, e := clock.Now()
		if e != nil || guard.Check(r) != nil {
			return ErrRefused
		}
		return nil
	}
	static := func() error {
		s, e := fixed.ReadStatic()
		if e != nil || s.LockSHA != v.LockSHA {
			return ErrRefused
		}
		return nil
	}
	acl := pathmeta.OSInspector{}
	publication := func(ctx context.Context, w contract.Witness, h contract.Handoff, complete bool) error {
		if fixed.CheckEvidenceDirectory() != nil || tick() != nil {
			return ErrRefused
		}
		raw, e := recordBytes(w, h, complete)
		if e != nil {
			return ErrRefused
		}
		name := intentName
		if complete {
			name = finalName
		}
		return publishRecord(ctx, contract.EvidenceRoot, 501, acl, name, raw, tick, func(f *os.File) error { return f.Close() }, func(f *os.File) error { return f.Sync() }, func(f *os.File, b []byte) (int, error) { return f.Write(b) })
	}
	doctor := func(ctx context.Context) error {
		p := contract.PackageRoot + "/doctor-state"
		if tick() != nil || static() != nil || emptyDoctorState(ctx, p, 501, acl) != nil || tick() != nil {
			return ErrRefused
		}
		r, e := fixed.WaitChild(contract.StaticFilePath(0), []string{"--config", contract.StaticFilePath(12), "doctor"}, fixed.Environment(false), strings.NewReader(""), time.Unix(0, int64(v.Handoff.DeadlineWall())))
		if doctorReturn(r, e) != nil || tick() != nil || emptyDoctorState(ctx, p, 501, acl) != nil || static() != nil || tick() != nil {
			return ErrRefused
		}
		return nil
	}
	return runFlow(context.Background(), flowDependencies{
		load: func() (flowInput, error) {
			var e error
			v, e = fixed.ReadInputs()
			if e != nil || fixed.CheckPlatform() != nil {
				return flowInput{}, ErrRefused
			}
			guard = clock.New(v.Handoff)
			return flowInput{Handoff: v.Handoff, LockSHA: v.LockSHA, HandoffSHA: v.HandoffSHA}, nil
		},
		transition: fixed.ReadCloseoutTransition,
		completion: fixed.ReadCompletion,
		now:        clock.Now, static: static,
		archive: func(ctx context.Context, h contract.Handoff) error {
			if fixed.CheckEvidenceDirectory() != nil {
				return ErrRefused
			}
			return verifyArchive(ctx, contract.EvidenceRoot+"/"+contract.ArchiveDirectory, 501, acl, h)
		},
		reserve: func(ctx context.Context, w contract.Witness, h contract.Handoff) error {
			return publication(ctx, w, h, false)
		},
		absence:   func(ctx context.Context) error { return candidateAbsent(ctx, nativeInspector()) },
		doctor:    doctor,
		protected: func(ctx context.Context) error { return observeProtectedInventory(ctx, v.Protected) },
		inventory: func(ctx context.Context, h contract.Handoff, check func() error) (retirable, error) {
			if fixed.CheckStateDirectory() != nil || stateVolume(ctx) != nil || check() != nil {
				return nil, ErrRefused
			}
			return inventory(ctx, scope{root: contract.StateRoot, uid: 501, acl: acl, check: check, volume: func() error { return qualificationVolume(ctx) }}, h)
		},
		configs: func(ctx context.Context, check func() error) error {
			hashes := [5]string{v.Lock.Files[10].SHA, v.Lock.Files[11].SHA, v.Lock.Files[12].SHA, contract.StockConfigSHA, contract.CandidateConfigSHA}
			return retireEnrolled(ctx, contract.ConfigRoot, 501, acl, hashes, check, func(r *os.Root, n string) error { return r.Remove(n) })
		},
		finish: func(ctx context.Context, w contract.Witness, h contract.Handoff) error {
			return publication(ctx, w, h, true)
		},
	})
}

func stateVolume(ctx context.Context) (err error) {
	if ctx.Err() != nil || fixed.CheckStateDirectory() != nil {
		return ErrRefused
	}
	before, e := os.Lstat(contract.StateRoot)
	if e != nil {
		return ErrRefused
	}
	fd, e := syscall.Open(contract.StateRoot, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if e != nil {
		return ErrRefused
	}
	f := os.NewFile(uintptr(fd), "n1-closeout-state-volume")
	defer func() { err = errors.Join(err, f.Close()) }()
	opened, e := f.Stat()
	if e != nil || !sameFile(before, opened) {
		return ErrRefused
	}
	id, e := observeAPFS(f)
	after, se := f.Stat()
	visible, ve := os.Lstat(contract.StateRoot)
	if e != nil || se != nil || ve != nil || id.VolumeUUID != contract.VolumeUUID || !sameFile(before, after) || !sameFile(before, visible) || ctx.Err() != nil {
		return ErrRefused
	}
	return nil
}

// The qualified mount remains after the owned state root is retired. Its
// identity is checked independently without interpreting the deleted path.
func qualificationVolume(ctx context.Context) (err error) {
	p := filepath.Dir(contract.StateRoot)
	if ctx.Err() != nil {
		return ErrRefused
	}
	before, e := checkedParent(p, 501, pathmeta.OSInspector{})
	if e != nil {
		return ErrRefused
	}
	fd, e := syscall.Open(p, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if e != nil {
		return ErrRefused
	}
	f := os.NewFile(uintptr(fd), "n1-closeout-qualified-volume")
	defer func() { err = errors.Join(err, f.Close()) }()
	opened, e := f.Stat()
	if e != nil || !sameFile(before, opened) {
		return ErrRefused
	}
	id, e := observeAPFS(f)
	after, se := f.Stat()
	visible, ve := os.Lstat(p)
	if e != nil || se != nil || ve != nil || id.VolumeUUID != contract.VolumeUUID || !sameFile(before, after) || !sameFile(before, visible) || ctx.Err() != nil {
		return ErrRefused
	}
	return nil
}
