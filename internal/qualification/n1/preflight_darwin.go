//go:build darwin && cgo && n1diagnostic && n1cleanup && !n1candidate

package n1

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"github.com/weshofmann/boxwarden/internal/backend"
	"github.com/weshofmann/boxwarden/internal/hostx"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/clock"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/contract"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/fixed"
	"io"
	"os"
	"syscall"
	"time"
)

func RunStamp() error {
	if len(os.Args) != 2 || os.Args[1] != "--stamp" || os.Getuid() != 501 || os.Geteuid() != 501 {
		return ErrRefused
	}
	anchor, e := clock.Now()
	if e != nil {
		return ErrRefused
	}
	s, e := fixed.ReadStatic()
	if e != nil {
		return ErrRefused
	}
	var id [16]byte
	if _, e = rand.Read(id[:]); e != nil {
		return ErrRefused
	}
	id[6] = (id[6] & 15) | 64
	id[8] = (id[8] & 63) | 128
	h := hex.EncodeToString(id[:])
	uuid := h[:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
	w, e := stampWindow(anchor, uuid, s.LockSHA)
	if e != nil {
		return ErrRefused
	}
	g := clock.Guard{Window: w}
	now, e := clock.Now()
	if e != nil || g.Check(now) != nil || now.Wall-anchor.Wall >= contract.AttendanceNS || now.Continuous-anchor.Continuous >= contract.AttendanceNS {
		return ErrRefused
	}
	raw, e := contract.Encode(w)
	if e != nil || len(raw)+1 > 512 {
		return ErrRefused
	}
	raw = append(raw, '\n')
	n, e := os.Stdout.Write(raw)
	if e != nil || n != len(raw) {
		return ErrRefused
	}
	return nil
}
func RunPreflight(observer backend.Observer) error {
	if len(os.Args) != 2 || os.Args[1] != "--preflight" || os.Getuid() != 0 || os.Geteuid() != 0 {
		return ErrRefused
	}
	timer := time.AfterFunc(5*time.Second, func() { os.Stdin.Close() })
	raw, re := io.ReadAll(io.LimitReader(os.Stdin, 513))
	ce := os.Stdin.Close()
	timer.Stop()
	w, e := contract.ParseOrigin(raw)
	if re != nil || ce != nil || e != nil {
		return ErrRefused
	}
	ctx, cancel := context.WithDeadline(context.Background(), time.Unix(0, int64(w.StartedUnixNS+contract.AttendanceNS)))
	defer cancel()
	request := hostx.Request{ConfiguredStateRoots: []string{contract.StateRoot}, TartPath: "/Users/devel/Library/Application Support/boxwarden/toolchains/tart/2.32.1/tart.app/Contents/MacOS/tart", TartHome: "/Users/devel/Library/Application Support/boxwarden/tart", SoftnetPath: contract.PackageRoot + "/artifacts/softnet-diagnostic"}
	d := preflightDependencies{now: clock.Now, static: fixed.ReadStatic, doctor: func(ctx context.Context, s fixed.StaticInputs) error {
		if hostx.NewSystemDoctor().CheckDiagnosticPreflight(ctx, request) != nil || observer == nil {
			return ErrRefused
		}
		base, e := observer.Observe(ctx, contract.BaseName)
		if e != nil || !base.Exists || base.ObjectID != contract.BaseName || base.State != backend.ObjectStopped {
			return ErrRefused
		}
		return nil
	}, inventory: preflightInventory, census: func(ctx context.Context, s fixed.StaticInputs) error {
		self, e := nativeProcess(ctx, os.Getpid(), s.Catalogue)
		if e != nil {
			return ErrRefused
		}
		return preflightCensus(ctx, nativeSampler{s.Catalogue}, s.Lock, self, s.Catalogue)
	}}
	// Only actual exit status relays read-only success into the owner's literal
	// Terminal && sequence. No receipt can authorize L/C or mutation by itself.
	return executePreflight(ctx, w, d)
}
func preflightInventory(ctx context.Context, s fixed.StaticInputs) error {
	if ctx.Err() != nil {
		return ErrRefused
	}
	for _, p := range []string{contract.StateRoot, contract.EvidenceRoot, contract.ConfigRoot + "/stock.enrolled.json", contract.ConfigRoot + "/candidate.enrolled.json"} {
		if _, e := os.Lstat(p); !os.IsNotExist(e) {
			return ErrRefused
		}
	}
	mount := "/Volumes/BoxwardenAlphaQualification"
	f, e := os.Open(mount)
	if e != nil {
		return ErrRefused
	}
	st, e := f.Stat()
	var fs syscall.Statfs_t
	fe := syscall.Fstatfs(int(f.Fd()), &fs)
	ce := f.Close()
	if e != nil || fe != nil || ce != nil || !st.IsDir() {
		return ErrRefused
	}
	// Encrypted mounted-volume association and selected stopped-base/protected
	// inventory are validated by the existing read-only storage/base boundary.
	if preflightStorage(ctx, mount, s) != nil {
		return ErrRefused
	}
	return observeProtectedInventory(ctx, s.Protected)
}
