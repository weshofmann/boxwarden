//go:build darwin && cgo

package adr024

import (
	"context"
	"os"
	"testing"
)

func TestSystemSamplerReadsCurrentProcessWithoutMutation(t *testing.T) {
	sampler, err := newSystemSampler()
	if err != nil {
		t.Fatalf("newSystemSampler() error = %v", err)
	}
	sample, err := sampler.Sample(context.Background(), os.Getpid())
	if err != nil {
		t.Fatalf("Sample(self) error = %v", err)
	}
	if sample.Tart.PID != os.Getpid() || sample.Tart.PPID != os.Getppid() {
		t.Fatalf("process identity = pid %d ppid %d", sample.Tart.PID, sample.Tart.PPID)
	}
	if sample.Tart.UniqueID == 0 || sample.Tart.ParentUniqueID == 0 || sample.Tart.StartTimeUnixMicros <= 0 || sample.Tart.Executable == "" {
		t.Fatalf("stable identity incomplete: %#v", sample.Tart)
	}
	if sample.Tart.Credentials != (Credentials{
		EffectiveUID: os.Geteuid(), EffectiveGID: os.Getegid(),
		RealUID: os.Getuid(), RealGID: os.Getgid(),
		SavedUID: os.Geteuid(), SavedGID: os.Getegid(),
	}) {
		t.Fatalf("credentials = %#v", sample.Tart.Credentials)
	}
}
