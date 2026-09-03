//go:build !darwin || !cgo

package adr024

import "errors"

func newSystemSampler() (Sampler, error) {
	return nil, errors.New("ADR 024 process observation requires Darwin with cgo-enabled libproc support")
}
