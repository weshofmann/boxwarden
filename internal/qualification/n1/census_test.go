package n1

import (
	"context"
	"errors"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/contract"
	"testing"
)

type censusFake struct {
	calls  int
	values [][]process
	err    error
}

func (f *censusFake) snapshot(context.Context) ([]process, error) {
	if f.err != nil {
		return nil, f.err
	}
	i := f.calls
	f.calls++
	if i >= len(f.values) {
		i = len(f.values) - 1
	}
	return f.values[i], nil
}
func TestCensusRejectsUnknownControllerChurnOverflowAndUnrelatedTart(t *testing.T) {
	safe := process{PID: 123, Birth: 1, Unique: 7, Kind: "digest", Path: contract.ArtifactPath(2), SHA: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Device: 1, Inode: 2}
	l, c := censusFixture()
	l.Artifacts[2].SHA = safe.SHA
	self := process{PID: 999, Birth: 9, Unique: 99, Kind: "digest", Path: contract.ArtifactPath(4), SHA: "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc", Device: 1, Inode: 3}
	l.Artifacts[4].SHA = self.SHA
	for _, which := range []string{"safe", "controller", "tart", "unknown", "pid0", "churn", "empty", "overflow", "denied", "missing-self"} {
		t.Run(which, func(t *testing.T) {
			p := safe
			var xs []process
			switch which {
			case "controller":
				p.SHA = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
				l.Artifacts[1].SHA = p.SHA
			case "tart":
				p.SHA = "05b65d5c14e8b41e8e44b6d9fd1278de4bedbc8b735d9b99f3c748f76f75862d"
			case "unknown":
				p.SHA = "unknown"
			case "pid0":
				p.PID = 0
			}
			xs = []process{p, self, fixtureKernel()}
			if which == "missing-self" {
				xs = []process{p}
			}
			if which == "empty" {
				xs = nil
			}
			if which == "overflow" {
				xs = make([]process, 8193)
			}
			f := &censusFake{values: [][]process{xs, xs}}
			if which == "churn" {
				q := p
				q.Birth++
				f.values[1] = []process{q, self, fixtureKernel()}
			}
			if which == "denied" {
				f.err = errors.New("denied")
			}
			e := census(t.Context(), f, l, self, c)
			if which == "safe" && e != nil {
				t.Fatal(e)
			}
			if which != "safe" && e == nil {
				t.Fatal("uncertain census accepted")
			}
		})
	}
}

func TestKernelObservationRequiresExactlyOneRetainedZero(t *testing.T) {
	l, c := censusFixture()
	self := process{PID: 999, Birth: 9, Unique: 99, Kind: "digest", Path: contract.ArtifactPath(4), SHA: l.Artifacts[4].SHA, Device: 1, Inode: 3}
	kernel := fixtureKernel()
	xs := []process{self, kernel}
	if e := census(t.Context(), &censusFake{values: [][]process{xs, xs}}, l, self, c); e != nil {
		t.Fatal("valid retained kernel refused", e)
	}
}

func fixtureKernel() process {
	k := kernelObservation{Status: 2, Flags: 17, Comm: "kernel_task", Name: "kernel_task", Birth: 1}
	return process{PID: 0, Birth: 1, Kind: "kernel", Kernel: k}
}
