package n1

import (
	"context"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/contract"
	"testing"
)

func TestPreflightCensusRefusesCleanupConsumers(t *testing.T) {
	for _, index := range []int{2, 3} {
		l, c := censusFixture()
		self := process{PID: 999, Birth: 9, Unique: 99, Kind: "digest", Path: contract.ArtifactPath(4), SHA: l.Artifacts[4].SHA, Device: 1, Inode: 3}
		actor := process{PID: 123, Birth: 1, Unique: 7, Kind: "digest", Path: contract.ArtifactPath(index), SHA: l.Artifacts[index].SHA, Device: 1, Inode: 2}
		xs := []process{self, actor}
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
