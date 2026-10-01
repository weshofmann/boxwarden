//go:build n1diagnostic && !n1candidate && (!darwin || !cgo)

package tart

import "github.com/weshofmann/boxwarden/internal/networkdiag"

func readDiagnosticProcess(uint32) (networkdiag.ProcessCorrelation, error) {
	return networkdiag.ProcessCorrelation{}, networkdiag.ErrMetadata
}
