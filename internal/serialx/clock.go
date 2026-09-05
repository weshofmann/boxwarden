package serialx

import "time"

// Clock supplies the single automation deadline and is injected for deterministic tests.
type Clock interface {
	After(time.Duration) <-chan time.Time
}
type systemClock struct{}

func (systemClock) After(delay time.Duration) <-chan time.Time { return time.After(delay) }
