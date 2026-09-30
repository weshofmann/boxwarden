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
	mu                                               sync.Mutex
	file                                             *os.File
	generation, nonce                                string
	mac                                              [6]uint8
	now                                              func() time.Time
	deadline, armAt                                  time.Time
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
	return newWatch(file, generation, nonce, mac, time.Now)
}
func newWatch(file *os.File, generation, nonce string, mac [6]uint8, now func() time.Time) *Watch {
	w := &Watch{file: file, generation: generation, nonce: nonce, mac: mac, now: now, deadline: now().Add(30 * time.Second), phase: "hello", helloDone: make(chan struct{}), armSent: make(chan struct{}), armedDone: make(chan struct{}), invalidDone: make(chan struct{}), done: make(chan struct{})}
	go w.read()
	return w
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
	if !w.now().Before(deadline) || w.file.SetReadDeadline(deadline) != nil {
		return nil, ErrMetadata
	}
	raw, err := ReadFrame(w.file)
	if err != nil || !w.now().Before(deadline) {
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
	if w.phase != "hello" || !w.now().Before(w.deadline) {
		w.mu.Unlock()
		w.invalid()
		return
	}
	w.hello = hello
	w.phase = "available"
	helloDeadline := w.deadline
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
	if w.phase != "arming" || !w.now().Before(deadline) {
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
	if w.phase != "armed" || !w.now().Before(deadline) {
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
	if w.phase != "available" || !w.now().Before(w.deadline) {
		return Hello{}, ErrMetadata
	}
	return w.hello, nil
}
func (w *Watch) Arm(ctx context.Context, a Arm) (Armed, error) {
	w.mu.Lock()
	if w.phase != "available" || ctx.Err() != nil || !w.now().Before(w.deadline) || !a.Valid(w.generation, w.nonce, w.mac, w.hello.Gateway) {
		w.mu.Unlock()
		w.invalid()
		return Armed{}, ErrMetadata
	}
	// Consume before the first syscall. No partial/error/canceled write retries.
	w.phase = "arming"
	w.arm = a
	w.armAt = w.now()
	w.deadline = w.armAt.Add(time.Duration(a.DurationMS)*time.Millisecond + 100*time.Millisecond)
	w.mu.Unlock()
	raw, err := Frame(a)
	if err == nil {
		var n int
		n, err = syscall.Write(int(w.file.Fd()), raw)
		if n != len(raw) {
			err = ErrMetadata
		}
	}
	if err != nil || ctx.Err() != nil {
		w.invalid()
	}
	close(w.armSent)
	select {
	case <-ctx.Done():
		w.invalid()
		return Armed{}, ctx.Err()
	case <-w.armedDone:
	}
	if w.afterArmedWait != nil {
		w.afterArmedWait()
	}
	return w.armResult()
}
func (w *Watch) armResult() (Armed, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.now().Before(w.deadline) {
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
	w.closeOnce.Do(func() { w.invalid(); w.closeErr = w.file.Close(); <-w.done })
	return w.closeErr
}
