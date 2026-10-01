//go:build n1diagnostic && !n1candidate && (!darwin || !cgo)

package networkdiag

func CurrentOwnerProcess() (ProcessCorrelation, error) { return ProcessCorrelation{}, ErrMetadata }
