package serialx

import "time"

// Clock is injected so deadline behavior is deterministic in tests and the
// serial package never reaches into global time state.
type Clock interface {
	After(time.Duration) <-chan time.Time
}

type systemClock struct{}

func (systemClock) After(delay time.Duration) <-chan time.Time { return time.After(delay) }
