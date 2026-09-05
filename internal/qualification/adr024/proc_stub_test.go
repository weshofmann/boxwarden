//go:build !darwin || !cgo

package adr024

import (
	"testing"
)

func TestSystemSamplerRefusesUnsupportedPlatform(t *testing.T) {
	sampler, err := newSystemSampler()
	if err == nil || sampler != nil {
		t.Fatalf("newSystemSampler() = %#v, %v; want deterministic refusal", sampler, err)
	}
}
