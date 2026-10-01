package n1

import (
	"context"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/clock"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/contract"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/fixed"
	"strings"
	"testing"
)

func TestPreflightCensusRefusesCleanupConsumers(t *testing.T) {
	for _, index := range []int{2, 3} {
		l, c := censusFixture()
		self := process{PID: 999, Birth: 9, Unique: 99, Kind: "digest", Path: contract.ArtifactPath(4), SHA: l.Artifacts[4].SHA, Device: 1, Inode: 3}
		actor := process{PID: 123, Birth: 1, Unique: 7, Kind: "digest", Path: contract.ArtifactPath(index), SHA: l.Artifacts[index].SHA, Device: 1, Inode: 2}
		xs := []process{self, actor, fixtureKernel()}
		if census(t.Context(), &censusFake{values: [][]process{xs, xs}}, l, self, c) != nil {
			t.Fatal("cleanup consumer refused")
		}
		if preflightCensus(t.Context(), &censusFake{values: [][]process{xs, xs}}, l, self, c) == nil {
			t.Errorf("preflight admitted cleanup actor%d", index)
		}
	}
}

func TestPreflightAndCleanupShareConservativeSystemAdmission(t *testing.T) {
	l, c := censusFixture()
	self := process{PID: 999, Birth: 9, Unique: 99, Kind: "digest", Path: contract.ArtifactPath(4), SHA: l.Artifacts[4].SHA, Device: 1, Inode: 3}
	base := []process{self}
	paths := append([]string{contract.SudoPath}, contract.SystemPaths[:]...)
	for j, p := range paths {
		q, key, ok := c.Lookup(p)
		if !ok {
			t.Fatal("fixture catalogue")
		}
		base = append(base, process{PID: 100 + j, Birth: uint64(j + 1), Unique: uint64(j + 40), Path: p, SHA: q.SHA, Device: q.Leaf.Device, Inode: q.Leaf.Inode, Kind: q.Kind, QualificationSHA: key})
	}
	base = append(base, fixtureKernel())
	for _, fn := range []func(context.Context, sampler, contract.StaticLock, process, contract.Catalogue) error{census, preflightCensus} {
		for _, mode := range []string{"positive", "unknown", "L", "C", "F", "tart", "softnet", "missing-self", "duplicate", "extra-H", "birth-zero", "unique-zero", "pid-zero", "wrong-path", "wrong-kind", "wrong-qualification", "device", "inode", "churn", "empty", "overflow", "sampler-error"} {
			t.Run(mode, func(t *testing.T) {
				xs := append([]process(nil), base...)
				switch mode {
				case "unknown":
					xs[1].Path = "/unknown/image"
				case "L", "C", "F":
					i := map[string]int{"L": 0, "C": 1, "F": 5}[mode]
					xs[1].Path = contract.ArtifactPath(i)
					xs[1].SHA = l.Artifacts[i].SHA
					xs[1].Kind = "digest"
					xs[1].QualificationSHA = ""
				case "tart":
					xs[1].SHA = "05b65d5c14e8b41e8e44b6d9fd1278de4bedbc8b735d9b99f3c748f76f75862d"
				case "softnet":
					xs[1].SHA = contract.SoftnetSHA
				case "missing-self":
					xs = xs[1:]
				case "duplicate":
					xs = append(xs, self)
				case "extra-H":
					p := self
					p.PID++
					xs = append(xs, p)
				case "birth-zero":
					xs[1].Birth = 0
				case "unique-zero":
					xs[1].Unique = 0
				case "pid-zero":
					xs[1].PID = 0
				case "wrong-path":
					xs[1].Path += "/foreign"
				case "wrong-kind":
					xs[1].Kind = "unknown"
				case "wrong-qualification":
					xs[1].QualificationSHA = l.Artifacts[4].SHA
				case "device":
					xs[1].Device++
				case "inode":
					xs[1].Inode++
				case "empty":
					xs = nil
				case "overflow":
					xs = make([]process, maxProcesses+1)
				}
				second := append([]process(nil), xs...)
				if mode == "churn" {
					second[1].Birth++
				}
				f := &censusFake{values: [][]process{xs, second}}
				if mode == "sampler-error" {
					f.err = ErrRefused
				}
				e := fn(t.Context(), f, l, self, c)
				if mode == "positive" {
					if e != nil || f.calls != 2 {
						t.Fatal(e, f.calls)
					}
				} else if e == nil {
					t.Fatal("uncertain system observation admitted")
				}
			})
		}
	}
	if censusInPhase(t.Context(), &censusFake{values: [][]process{base, base}}, l, self, c, censusPhase(255)) == nil {
		t.Fatal("unknown phase admitted")
	}
}

func TestKernelRecordCannotBeSkippedBorrowExecutableOrDrift(t *testing.T) {
	l, c := censusFixture()
	self := process{PID: 999, Birth: 9, Unique: 99, Kind: "digest", Path: contract.ArtifactPath(4), SHA: l.Artifacts[4].SHA, Device: 1, Inode: 3}
	for _, mode := range []string{"positive", "missing", "duplicate", "negative", "fake-positive", "unique", "path", "SHA", "device", "inode", "qualification", "kind", "birth", "drift", "second-missing", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			k := fixtureKernel()
			xs := []process{self, k}
			switch mode {
			case "missing":
				xs = xs[:1]
			case "duplicate":
				xs = append(xs, k)
			case "negative":
				xs[1].PID = -1
			case "fake-positive":
				xs[1].PID = 1
			case "unique":
				xs[1].Unique = 1
			case "path":
				xs[1].Path = "/sbin/launchd"
			case "SHA":
				xs[1].SHA = l.Artifacts[4].SHA
			case "device":
				xs[1].Device = 1
			case "inode":
				xs[1].Inode = 1
			case "qualification":
				xs[1].QualificationSHA = l.ProtectedSudo.QualificationSHA
			case "kind":
				xs[1].Kind = "digest"
			case "birth":
				xs[1].Birth++
			case "cancel":
				cancel()
			}
			second := append([]process(nil), xs...)
			if mode == "drift" {
				second[1].Kernel.Birth++
				second[1].Birth++
			}
			if mode == "second-missing" {
				second = second[:1]
			}
			f := &censusFake{values: [][]process{xs, second}}
			e := preflightCensus(ctx, f, l, self, c)
			if mode == "positive" {
				if e != nil || f.calls != 2 {
					t.Fatal(e, f.calls)
				}
			} else if e == nil {
				t.Fatal("invalid kernel became admitted consumer")
			}
		})
	}
}

func TestPreflightComposesPagedCatalogueAndKernelWithoutDispatch(t *testing.T) {
	l, c := censusFixture()
	w, e := stampWindow(clock.Reading{Wall: 100, Continuous: 200}, "11111111-1111-4111-8111-111111111111", strings.Repeat("a", 64))
	if e != nil {
		t.Fatal(e)
	}
	original := w
	calls := []string{}
	self := process{PID: 999, Birth: 9, Unique: 99, Kind: "digest", Path: contract.ArtifactPath(4), SHA: l.Artifacts[4].SHA, Device: 1, Inode: 3}
	kernel := fixtureKernel()
	xs := []process{self, kernel}
	sampler := &censusFake{values: [][]process{xs, xs}}
	// The production dependency surface accepts observations only. All supplied
	// implementations are pure fixtures; no install/dispatch/mutation hook exists.
	d := preflightDependencies{now: func() (clock.Reading, error) {
		return clock.Reading{Wall: 100 + 9*60*1000000000, Continuous: 200 + 9*60*1000000000}, nil
	}, static: func() (fixed.StaticInputs, error) {
		calls = append(calls, "static")
		return fixed.StaticInputs{Lock: l, Catalogue: c, Protected: protectedFixture(), Build: buildFixture(), LockSHA: w.LockSHA}, nil
	}, doctor: func(context.Context, fixed.StaticInputs) error { calls = append(calls, "doctor"); return nil }, inventory: func(context.Context, fixed.StaticInputs) error { calls = append(calls, "inventory"); return nil }, census: func(ctx context.Context, in fixed.StaticInputs) error {
		calls = append(calls, "census")
		return preflightCensus(ctx, sampler, in.Lock, self, in.Catalogue)
	}}
	if e = executePreflight(t.Context(), w, d); e != nil || strings.Join(calls, ",") != "static,doctor,inventory,census" || sampler.calls != 2 {
		t.Fatal(e, calls, sampler.calls)
	}
	if w != original || w.ExpiresUnixNS != 1800000000100 || w.ContinuousLimitNS != 1800000000200 || l.ActiveSeconds != 1200 || l.AttendanceSeconds != 600 {
		t.Fatal("original budgets changed")
	}
}
