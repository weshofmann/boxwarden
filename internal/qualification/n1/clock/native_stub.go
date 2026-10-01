//go:build !darwin || !cgo

package clock

func Now() (Reading, error) { return Reading{}, ErrClock }
