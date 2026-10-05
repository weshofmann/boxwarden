//go:build !darwin || !cgo

package hostidentity

import "fmt"

func observeFirstRunLocation(string, string) (LocationObservation, error) {
	return LocationObservation{}, fmt.Errorf("first-run storage inspection requires macOS with cgo")
}
func mountedFirstRunLocations(string) ([]string, error) {
	return nil, fmt.Errorf("first-run storage inspection requires macOS with cgo")
}
