//go:build !darwin || !cgo

package serialx

import "fmt"

func productionRuntimeDeps() (runtimeDeps, error) {
	return runtimeDeps{}, fmt.Errorf("Screen runtime is available only on Darwin with cgo")
}

func productionRuntimeSupported() bool { return false }
