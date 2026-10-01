//go:build darwin && cgo && n1diagnostic && n1cleanup && !n1candidate

package n1

import (
	"context"
	"github.com/weshofmann/boxwarden/internal/hostx"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/clock"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/contract"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/fixed"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/receipt"
	"os"
	"time"
)

func RunCleanup() error {
	if len(os.Args) != 1 || os.Getuid() != 0 || os.Geteuid() != 0 {
		return ErrRefused
	}
	v, e := fixed.ReadInputs()
	if e != nil || fixed.CheckArtifact(4, v.Lock) != nil {
		return ErrRefused
	}
	timeGuard := clock.New(v.Handoff)
	check := func(ctx context.Context) error {
		r, e := clock.Now()
		if ctx.Err() != nil || e != nil || timeGuard.Check(r) != nil {
			return ErrRefused
		}
		now, e := fixed.ReadInputs()
		if e != nil || now.LockSHA != v.LockSHA || now.HandoffSHA != v.HandoffSHA {
			return ErrRefused
		}
		return nil
	}
	ctx, cancel := context.WithDeadline(context.Background(), time.Unix(0, int64(v.Handoff.DeadlineWall())))
	defer cancel()
	if check(ctx) != nil {
		return ErrRefused
	}
	self, e := nativeProcess(ctx, os.Getpid(), v.Catalogue)
	if e != nil || self.SHA != v.Lock.Artifacts[4].SHA || self.Path != contract.ArtifactPath(4) {
		return ErrRefused
	}
	request := hostx.Request{ConfiguredStateRoots: []string{contract.StateRoot}, TartPath: "/Users/devel/Library/Application Support/boxwarden/toolchains/tart/2.32.1/tart.app/Contents/MacOS/tart", TartHome: "/Users/devel/Library/Application Support/boxwarden/tart", SoftnetPath: contract.PackageRoot + "/artifacts/softnet-diagnostic"}
	d := cleanupDependencies{acquire: func(ctx context.Context) (cleanupGuard, error) {
		return hostx.NewSystemDoctor().AcquireDiagnosticCleanup(ctx, request)
	}, inventory: func(ctx context.Context) (stateGuard, error) {
		return inventory(ctx, scope{root: contract.StateRoot, uid: 501, acl: hostx.OSACLInspector{}}, v.Handoff)
	}, storage: storage, census: func(ctx context.Context) error {
		return census(ctx, nativeSampler{v.Catalogue}, v.Lock, self, v.Catalogue)
	}, check: check, publish: receipt.Publish}
	if executeCleanup(ctx, v, d) != nil || check(ctx) != nil {
		return ErrRefused
	}
	_, sha, e := fixed.ReadCompletion()
	if e != nil {
		return ErrRefused
	}
	return fixed.WriteWitness(contract.Witness{Version: 1, Phase: 2, LockSHA: v.LockSHA, WindowID: v.Handoff.Window.ID, HandoffSHA: v.HandoffSHA, CompletionSHA: sha, Exit: 0})
}
