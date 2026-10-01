//go:build (n1diagnostic || n1clipboarddiagnostic) && !n1candidate

package networkdiag

import (
	"sync"
	"time"
)

const HelloLimit = 30 * time.Second
const PrearmCap = 30 * time.Minute

// ClockReading uses the host's Unix wall and suspend-aware continuous clocks.
// Neither reading is a Softnet/guest offset or an owner approval timestamp.
type ClockReading struct {
	WallNS       uint64 `json:"wall_ns"`
	ContinuousNS uint64 `json:"continuous_ns"`
}
type LaunchClock struct {
	mu           sync.Mutex
	anchor, last ClockReading
	read         func() (ClockReading, error)
	invalid      bool
}

func deadlineReading(r ClockReading, d time.Duration) (ClockReading, error) {
	n := uint64(d)
	if d <= 0 || r.WallNS == 0 || r.ContinuousNS == 0 || r.WallNS > uint64(1<<63-1)-n || r.ContinuousNS > ^uint64(0)-n {
		return ClockReading{}, ErrMetadata
	}
	return ClockReading{r.WallNS + n, r.ContinuousNS + n}, nil
}

// NewLaunchClock must run before the actual retained spawn attempt.
func NewLaunchClock() (*LaunchClock, error) { return newLaunchClock(nativeClock) }
func newLaunchClock(read func() (ClockReading, error)) (*LaunchClock, error) {
	if read == nil {
		return nil, ErrMetadata
	}
	r, e := read()
	if e != nil {
		return nil, ErrMetadata
	}
	if _, e = deadlineReading(r, PrearmCap); e != nil {
		return nil, e
	}
	return &LaunchClock{anchor: r, last: r, read: read}, nil
}
func (c *LaunchClock) check(prearm bool) (ClockReading, error) {
	if c == nil {
		return ClockReading{}, ErrMetadata
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.invalid {
		return ClockReading{}, ErrMetadata
	}
	r, e := c.read()
	cap, _ := deadlineReading(c.anchor, PrearmCap)
	if e != nil || r.WallNS == 0 || r.ContinuousNS == 0 || r.WallNS < c.last.WallNS || r.ContinuousNS < c.last.ContinuousNS || prearm && (r.WallNS >= cap.WallNS || r.ContinuousNS >= cap.ContinuousNS) {
		c.invalid = true
		return ClockReading{}, ErrMetadata
	}
	c.last = r
	return r, nil
}
func beforeReading(r, d ClockReading) bool {
	return r.WallNS < d.WallNS && r.ContinuousNS < d.ContinuousNS
}

// HostClockNow is a read-only host observation, with no launch/deadline state.
func HostClockNow() (ClockReading, error) { return nativeClock() }
