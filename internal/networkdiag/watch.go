//go:build n1diagnostic && !n1candidate

package networkdiag

import (
	"context"
	"errors"
	"os"
	"sync"
	"syscall"
	"time"
)

type Watch struct {
	// Private scheduling seam used only by same-package synthetic controls.
	afterArmedWait                                   func()
	afterHelloDecode                                 func()
	mu                                               sync.Mutex
	file                                             *os.File
	generation, nonce                                string
	mac                                              [6]uint8
	now                                              func() time.Time
	deadline, armAt                                  time.Time
	clock                                            *LaunchClock
	helloDeadline, resourceDeadline, armDeadline     ClockReading
	armReceipt                                       ArmReceipt
	timerDone                                        chan struct{}
	hello                                            Hello
	arm                                              Arm
	armed                                            Armed
	summary                                          Summary
	phase                                            string
	helloDone, armSent, armedDone, invalidDone, done chan struct{}
	helloOnce, armedOnce, invalidOnce, closeOnce     sync.Once
	closeErr                                         error
}

func NewWatch(file *os.File, generation, nonce string, mac [6]uint8) *Watch {
	c, _ := NewLaunchClock()
	return NewWatchAt(file, generation, nonce, mac, c)
}

// NewWatchAt consumes the launch anchor established before spawn; no later
// observation, HELLO, READY or ARM caller can reset the resource cap.
func NewWatchAt(file *os.File, generation, nonce string, mac [6]uint8, c *LaunchClock) *Watch {
	return watchAt(file, generation, nonce, mac, c, time.Now)
}
func newWatch(file *os.File, generation, nonce string, mac [6]uint8, now func() time.Time) *Watch {
	c, _ := newLaunchClock(func() (ClockReading, error) {
		n := now().UnixNano()
		if n <= 0 {
			return ClockReading{}, ErrMetadata
		}
		return ClockReading{uint64(n), uint64(n)}, nil
	})
	return watchAt(file, generation, nonce, mac, c, now)
}
func watchAt(file *os.File, generation, nonce string, mac [6]uint8, c *LaunchClock, now func() time.Time) *Watch {
	w := &Watch{file: file, generation: generation, nonce: nonce, mac: mac, clock: c, now: now, phase: "hello", helloDone: make(chan struct{}), armSent: make(chan struct{}), armedDone: make(chan struct{}), invalidDone: make(chan struct{}), done: make(chan struct{}), timerDone: make(chan struct{})}
	if c != nil {
		w.helloDeadline, _ = deadlineReading(c.anchor, HelloLimit)
		w.resourceDeadline, _ = deadlineReading(c.anchor, PrearmCap)
		w.deadline = time.Unix(0, int64(w.helloDeadline.WallNS))
	}
	go w.read()
	go w.expire()
	return w
}
func (w *Watch) validClockLocked() bool {
	if w.clock == nil {
		return w.now != nil && w.now().Before(w.deadline)
	} // legacy private frame fixtures only
	prearm := w.phase == "hello" || w.phase == "available"
	r, e := w.clock.check(prearm)
	if e != nil {
		return false
	}
	if w.phase == "hello" {
		return beforeReading(r, w.helloDeadline)
	}
	if w.phase == "arming" || w.phase == "armed" {
		return beforeReading(r, w.armDeadline)
	}
	return w.phase != "invalid"
}
func (w *Watch) expire() {
	defer close(w.timerDone)
	tick := time.NewTicker(25 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-w.done:
			return
		case <-w.invalidDone:
			return
		case <-tick.C:
			w.mu.Lock()
			valid := w.validClockLocked()
			w.mu.Unlock()
			if !valid {
				w.invalid()
				return
			}
		}
	}
}
func (w *Watch) invalid() {
	w.mu.Lock()
	w.phase = "invalid"
	w.mu.Unlock()
	w.file.SetReadDeadline(time.Now())
	w.invalidOnce.Do(func() { close(w.invalidDone) })
	w.helloOnce.Do(func() { close(w.helloDone) })
	w.armedOnce.Do(func() { close(w.armedDone) })
}
func (w *Watch) receive(deadline time.Time, total *int) ([]byte, error) {
	w.mu.Lock()
	valid := w.validClockLocked()
	w.mu.Unlock()
	if !valid || !w.now().Before(deadline) || w.file.SetReadDeadline(deadline) != nil {
		return nil, ErrMetadata
	}
	raw, err := ReadFrame(w.file)
	w.mu.Lock()
	valid = w.validClockLocked()
	w.mu.Unlock()
	if err != nil || !valid || !w.now().Before(deadline) {
		return nil, ErrMetadata
	}
	*total += len(raw) + 4
	if *total > MaxChildOutput {
		return nil, ErrMetadata
	}
	return raw, nil
}
func (w *Watch) read() {
	defer close(w.done)
	defer w.helloOnce.Do(func() { close(w.helloDone) })
	defer w.armedOnce.Do(func() { close(w.armedDone) })
	total := 0
	raw, err := w.receive(w.deadline, &total)
	var hello Hello
	if err != nil || Decode(raw, &hello) != nil || hello.Version != 1 || hello.Kind != "HELLO" || hello.Generation != w.generation || hello.Nonce != w.nonce || hello.CandidateMAC != w.mac || !MAC(w.mac) || !Private(hello.Gateway) {
		w.invalid()
		return
	}
	w.mu.Lock()
	decoded := w.afterHelloDecode
	w.mu.Unlock()
	if decoded != nil {
		decoded()
	}
	w.mu.Lock()
	if w.phase != "hello" || !w.validClockLocked() {
		w.mu.Unlock()
		w.invalid()
		return
	}
	w.hello = hello
	w.phase = "available"
	helloDeadline := time.Unix(0, int64(w.resourceDeadline.WallNS))
	w.mu.Unlock()
	w.helloOnce.Do(func() { close(w.helloDone) })
	timer := time.NewTimer(time.Until(helloDeadline))
	defer timer.Stop()
	select {
	case <-w.armSent:
	case <-w.invalidDone:
		return
	case <-timer.C:
		w.invalid()
		return
	}
	w.mu.Lock()
	a, deadline, armAt, phase := w.arm, w.deadline, w.armAt, w.phase
	w.mu.Unlock()
	if phase != "arming" {
		return
	}
	raw, err = w.receive(deadline, &total)
	var armed Armed
	if err != nil || Decode(raw, &armed) != nil || !armed.Matches(a) {
		w.invalid()
		return
	}
	w.mu.Lock()
	if w.phase != "arming" || !w.validClockLocked() {
		w.mu.Unlock()
		w.invalid()
		return
	}
	w.armed = armed
	w.phase = "armed"
	w.mu.Unlock()
	w.armedOnce.Do(func() { close(w.armedDone) })
	raw, err = w.receive(deadline, &total)
	var summary Summary
	if err != nil || Decode(raw, &summary) != nil || !summary.Matches(a, armed) || w.now().Before(armAt.Add(time.Duration(a.DurationMS)*time.Millisecond)) {
		w.invalid()
		return
	}
	w.mu.Lock()
	if w.phase != "armed" || !w.validClockLocked() {
		w.mu.Unlock()
		w.invalid()
		return
	}
	w.summary = summary
	w.phase = "summary"
	w.mu.Unlock()
	// Keep the independent reader finite through the original close bound. A
	// disconnected collector cannot suspend accounting or admit an extra frame.
	var extra [1]byte
	n, readErr := w.file.Read(extra[:])
	if n != 0 || !errors.Is(readErr, os.ErrDeadlineExceeded) {
		w.invalid()
		return
	}
	w.mu.Lock()
	if w.phase == "summary" {
		w.phase = "complete"
	}
	w.mu.Unlock()
}
func (w *Watch) AwaitHello(ctx context.Context) (Hello, error) {
	select {
	case <-ctx.Done():
		return Hello{}, ctx.Err()
	case <-w.helloDone:
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.phase != "available" || !w.validClockLocked() {
		return Hello{}, ErrMetadata
	}
	return w.hello, nil
}
func (w *Watch) Arm(ctx context.Context, a Arm) (Armed, error) {
	r, e := w.ArmReceipt(ctx, a)
	return r.Armed, e
}
func (w *Watch) ArmReceipt(ctx context.Context, a Arm) (ArmReceipt, error) {
	w.mu.Lock()
	if w.phase != "available" || ctx.Err() != nil || !w.validClockLocked() || !a.Valid(w.generation, w.nonce, w.mac, w.hello.Gateway) {
		w.mu.Unlock()
		w.invalid()
		return ArmReceipt{}, ErrMetadata
	}
	raw, frameErr := Frame(a)
	if frameErr != nil {
		w.mu.Unlock()
		w.invalid()
		return ArmReceipt{}, ErrMetadata
	}
	// Consume before the first syscall. No partial/error/canceled write retries.
	w.phase = "arming"
	w.arm = a
	sent, e := w.clock.check(true)
	limit, de := deadlineReading(sent, time.Duration(a.DurationMS)*time.Millisecond+100*time.Millisecond)
	if e != nil || de != nil {
		w.mu.Unlock()
		w.invalid()
		return ArmReceipt{}, ErrMetadata
	}
	w.armAt = time.Unix(0, int64(sent.WallNS))
	w.armDeadline = limit
	w.deadline = time.Unix(0, int64(limit.WallNS))
	w.armReceipt = ArmReceipt{Version: 1, Sent: sent, Deadline: limit}
	w.mu.Unlock()
	n, err := syscall.Write(int(w.file.Fd()), raw)
	if n != len(raw) {
		err = ErrMetadata
	}
	w.mu.Lock()
	_, capErr := w.clock.check(true)
	valid := w.validClockLocked()
	w.mu.Unlock()
	if capErr != nil || !valid {
		err = ErrMetadata
	}
	if err != nil || ctx.Err() != nil {
		w.invalid()
	}
	close(w.armSent)
	select {
	case <-ctx.Done():
		w.invalid()
		return ArmReceipt{}, ctx.Err()
	case <-w.armedDone:
	}
	if w.afterArmedWait != nil {
		w.afterArmedWait()
	}
	armed, e := w.armResult()
	if e != nil {
		return ArmReceipt{}, e
	}
	w.mu.Lock()
	result := w.armReceipt
	result.Armed = armed
	w.mu.Unlock()
	return result, nil
}
func (w *Watch) armResult() (Armed, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	valid := w.validClockLocked()
	// An ARM caller must still be inside its original receipt deadline even
	// when the independent reader has already retained SUMMARY or completed.
	// Keep this bound specific to ARM results: completed collection is retained.
	if valid && w.clock != nil {
		current, err := w.clock.check(false)
		valid = err == nil && beforeReading(current, w.armDeadline)
	}
	if !valid {
		w.phase = "invalid"
		return Armed{}, ErrMetadata
	}
	if w.phase != "armed" && w.phase != "summary" && w.phase != "complete" {
		return Armed{}, ErrMetadata
	}
	return w.armed, nil
}
func (w *Watch) Invalidate() { w.invalid() }
func (w *Watch) Collect(ctx context.Context, operation string) (Summary, error) {
	if err := ctx.Err(); err != nil {
		return Summary{}, err
	}
	select {
	case <-ctx.Done():
		return Summary{}, ctx.Err()
	case <-w.done:
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.phase != "complete" || operation != w.arm.OperationID {
		return Summary{}, ErrMetadata
	}
	return w.summary, nil
}
func (w *Watch) Close() error {
	w.closeOnce.Do(func() { w.invalid(); w.closeErr = w.file.Close(); <-w.done; <-w.timerDone })
	return w.closeErr
}

// Observe does not wait for HELLO or mutate an available channel's deadline.
func (w *Watch) Observe() (WatchObservation, error) {
	if w == nil {
		return WatchObservation{}, ErrMetadata
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.phase != "available" || !w.validClockLocked() {
		return WatchObservation{}, ErrMetadata
	}
	r, e := w.clock.check(true)
	result := WatchObservation{Hello: w.hello, Phase: w.phase, Anchor: w.clock.anchor, Deadline: w.resourceDeadline, Observed: r}
	if e != nil || !result.Valid() {
		return WatchObservation{}, ErrMetadata
	}
	return result, nil
}
