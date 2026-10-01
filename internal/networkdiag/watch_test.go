//go:build n1diagnostic && !n1candidate

package networkdiag

import (
	"context"
	"encoding/binary"
	"errors"
	"os"
	"sync"
	"syscall"
	"testing"
	"time"
)

func watchFixture(t *testing.T, clock func() time.Time) (*Watch, *os.File) {
	t.Helper()
	fds, err := syscall.Socketpair(syscall.AF_UNIX, syscall.SOCK_STREAM, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, fd := range fds {
		syscall.CloseOnExec(fd)
		syscall.SetNonblock(fd, true)
	}
	a := armFixture()
	w := newWatch(os.NewFile(uintptr(fds[0]), "fixture-parent"), a.Generation, a.Nonce, a.Candidate.MAC, clock)
	child := os.NewFile(uintptr(fds[1]), "fixture-child")
	t.Cleanup(func() { w.Close(); child.Close() })
	child.SetDeadline(time.Now().Add(5 * time.Second))
	return w, child
}
func sendFixture(t *testing.T, f *os.File, v any) {
	t.Helper()
	raw, err := Frame(v)
	if err != nil {
		t.Fatal(err)
	}
	if n, err := f.Write(raw); err != nil || n != len(raw) {
		t.Fatal("fixture write", n, err)
	}
}
func helloFixture(a Arm) Hello {
	return Hello{1, "HELLO", a.Generation, a.Nonce, a.Candidate.MAC, a.Gateway}
}
func armedFixture(a Arm) Armed {
	return Armed{1, "ARMED", a.Generation, a.Nonce, a.OperationID, a.Candidate, a.Control, a.Gateway, a.DurationMS, a.ControlProvenance, 100, true}
}
func summaryFixture(a Arm) Summary {
	return Summary{Version: 1, Kind: "SUMMARY", Generation: a.Generation, Nonce: a.Nonce, OperationID: a.OperationID, Candidate: a.Candidate, Control: a.Control, Gateway: a.Gateway, DurationMS: a.DurationMS, ControlProvenance: a.ControlProvenance, ArmedOffsetNS: 100, EndOffsetNS: 100 + uint64(a.DurationMS)*1000000, CandidateLeaseValid: true, CoverageScope: "identified_pair_headers", PacketCountUnobserved: true, Complete: true}
}
func TestDiagnosticWatchFiniteIndependentExchange(t *testing.T) {
	w, child := watchFixture(t, time.Now)
	a := armFixture()
	sendFixture(t, child, helloFixture(a))
	if _, err := w.AwaitHello(t.Context()); err != nil {
		t.Fatal(err)
	}
	worker := make(chan error, 1)
	go func() {
		raw, err := ReadFrame(child)
		var got Arm
		if err == nil {
			err = Decode(raw, &got)
		}
		if err == nil && got != a {
			err = ErrMetadata
		}
		if err == nil {
			sendFixture(t, child, armedFixture(a))
			time.Sleep(time.Duration(a.DurationMS) * time.Millisecond)
			sendFixture(t, child, summaryFixture(a))
		}
		worker <- err
	}()
	if _, err := w.Arm(t.Context(), a); err != nil {
		t.Fatal(err)
	}
	if err := <-worker; err != nil {
		t.Fatal(err)
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := w.Collect(canceled, a.OperationID); err == nil {
		t.Fatal("canceled collector succeeded")
	}
	if got, err := w.Collect(t.Context(), a.OperationID); err != nil || !got.Complete {
		t.Fatalf("finite summary=%v", err)
	}
	if _, err := w.Arm(t.Context(), a); err == nil {
		t.Fatal("second ARM admitted")
	}
}
func TestDiagnosticWatchLateBufferedHelloSticky(t *testing.T) {
	var mu sync.Mutex
	now := time.Now()
	clock := func() time.Time { mu.Lock(); defer mu.Unlock(); return now }
	w, child := watchFixture(t, clock)
	a := armFixture()
	raw, _ := Frame(helloFixture(a))
	if _, err := child.Write(raw[:len(raw)-1]); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	now = now.Add(31 * time.Second)
	mu.Unlock()
	child.Write(raw[len(raw)-1:])
	if _, err := w.AwaitHello(t.Context()); err == nil {
		t.Fatal("late partial-final HELLO admitted")
	}
	if _, err := w.Arm(t.Context(), a); err == nil {
		t.Fatal("late HELLO rearmed")
	}
}
func TestDiagnosticFrameZeroOversizeAndPartial(t *testing.T) {
	for _, size := range []uint32{0, 4097} {
		var raw [4]byte
		binary.BigEndian.PutUint32(raw[:], size)
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		w.Write(raw[:])
		w.Close()
		if _, err := ReadFrame(r); err == nil {
			t.Fatal("bad length admitted")
		}
		r.Close()
	}
	if !errors.Is(ErrMetadata, ErrMetadata) {
		t.Fatal("fixed error identity")
	}
}
func TestDiagnosticWatchArmedWaiterOriginalDeadline(t *testing.T) {
	a := armFixture()
	deadline := time.Now().Add(time.Second)
	now := deadline.Add(-time.Nanosecond)
	w := &Watch{phase: "armed", armed: armedFixture(a), deadline: deadline, now: func() time.Time { return now }}
	if _, err := w.armResult(); err != nil {
		t.Fatal("timely ARMED unavailable")
	}
	now = deadline
	if _, err := w.armResult(); err == nil {
		t.Fatal("scheduled-late ARM waiter accepted prior receipt")
	}
	// Collection has its own retained-result contract and must not be made
	// fresh by this refusal or given a new watch deadline.
	if !w.deadline.Equal(deadline) {
		t.Fatal("deadline extended")
	}
}
func TestDiagnosticWatchDecodedArmedWaiterResumesLate(t *testing.T) {
	var clockMu sync.Mutex
	now := time.Now()
	clock := func() time.Time { clockMu.Lock(); defer clockMu.Unlock(); return now }
	w, child := watchFixture(t, clock)
	a := armFixture()
	sendFixture(t, child, helloFixture(a))
	if _, err := w.AwaitHello(t.Context()); err != nil {
		t.Fatal(err)
	}
	paused, resume := make(chan struct{}), make(chan struct{})
	w.afterArmedWait = func() { close(paused); <-resume }
	done := make(chan error, 1)
	go func() { _, err := w.Arm(t.Context(), a); done <- err }()
	if _, err := ReadFrame(child); err != nil {
		t.Fatal(err)
	}
	sendFixture(t, child, armedFixture(a))
	<-paused
	w.mu.Lock()
	deadline := w.deadline
	phase := w.phase
	w.mu.Unlock()
	if phase != "armed" {
		t.Fatalf("ARMED was not actually decoded on time: %s", phase)
	}
	clockMu.Lock()
	now = deadline
	clockMu.Unlock()
	close(resume)
	if err := <-done; err == nil {
		t.Fatal("late scheduled waiter accepted timely decoded ARMED")
	}
	w.mu.Lock()
	if !w.deadline.Equal(deadline) || w.phase != "invalid" {
		t.Fatal("deadline extended or terminal invalid not sticky")
	}
	w.mu.Unlock()
}
func TestDiagnosticWatchCancelBackpressureAndOrderAreSticky(t *testing.T) {
	for _, kind := range []string{"cancel", "backpressure", "order"} {
		t.Run(kind, func(t *testing.T) {
			w, child := watchFixture(t, time.Now)
			a := armFixture()
			sendFixture(t, child, helloFixture(a))
			if _, err := w.AwaitHello(t.Context()); err != nil {
				t.Fatal(err)
			}
			ctx := t.Context()
			if kind == "cancel" {
				c, cancel := context.WithCancel(ctx)
				cancel()
				ctx = c
			}
			if kind == "backpressure" {
				raw := make([]byte, 8192)
				for {
					_, err := syscall.Write(int(w.file.Fd()), raw)
					if err == syscall.EAGAIN {
						break
					}
					if err != nil {
						t.Fatal(err)
					}
				}
			}
			if kind == "order" {
				go func() { ReadFrame(child); sendFixture(t, child, helloFixture(a)) }()
			}
			if _, err := w.Arm(ctx, a); err == nil {
				t.Fatal("invalid ARM/exchange accepted")
			}
			if _, err := w.Arm(t.Context(), a); err == nil {
				t.Fatal("invalid generation rearmed")
			}
			if _, err := w.Collect(t.Context(), a.OperationID); err == nil {
				t.Fatal("sticky invalid returned summary")
			}
		})
	}
}
func TestDiagnosticWatchAggregateIncludesPrefixes(t *testing.T) {
	w, child := watchFixture(t, time.Now)
	a := armFixture()
	sendFixture(t, child, helloFixture(a))
	if _, err := w.AwaitHello(t.Context()); err != nil {
		t.Fatal(err)
	}
	total := MaxChildOutput - 1
	sendFixture(t, child, helloFixture(a))
	if _, err := w.receive(time.Now().Add(time.Second), &total); err == nil {
		t.Fatal("prefix-inclusive aggregate quota relaxed")
	}
}

func TestDiagnosticPartialFinalReceiptsUseOriginalDeadline(t *testing.T) {
	a := armFixture()
	for _, value := range []any{armedFixture(a), summaryFixture(a)} {
		t.Run(map[bool]string{true: "ARMED", false: "SUMMARY"}[value == any(armedFixture(a))], func(t *testing.T) {
			fds, err := syscall.Socketpair(syscall.AF_UNIX, syscall.SOCK_STREAM, 0)
			if err != nil {
				t.Fatal(err)
			}
			for _, fd := range fds {
				syscall.CloseOnExec(fd)
				syscall.SetNonblock(fd, true)
			}
			parent := os.NewFile(uintptr(fds[0]), "partial-parent")
			child := os.NewFile(uintptr(fds[1]), "partial-child")
			defer parent.Close()
			defer child.Close()
			raw, err := Frame(value)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = child.Write(raw[:len(raw)-1]); err != nil {
				t.Fatal(err)
			}
			var mu sync.Mutex
			deadline := time.Now().Add(time.Second)
			now := deadline.Add(-time.Nanosecond)
			entered := make(chan struct{})
			once := sync.Once{}
			w := &Watch{file: parent, now: func() time.Time { mu.Lock(); defer mu.Unlock(); once.Do(func() { close(entered) }); return now }}
			done := make(chan error, 1)
			go func() { total := 0; _, e := w.receive(deadline, &total); done <- e }()
			<-entered
			mu.Lock()
			now = deadline
			mu.Unlock()
			if _, err = child.Write(raw[len(raw)-1:]); err != nil {
				t.Fatal(err)
			}
			if err = <-done; err == nil {
				t.Fatal("late buffered partial-final receipt accepted")
			}
		})
	}
}

func TestDiagnosticWatchConsumesCompleteCounterImplications(t *testing.T) {
	for _, mode := range []string{"vm_refresh_failure", "host_refresh_failure", "unidentified_refresh_failure", "vm_length_mismatch", "host_length_mismatch", "valid_zero", "valid_vm_api_full", "valid_vm_denial", "valid_host_api_full", "valid_host_denial"} {
		t.Run(mode, func(t *testing.T) {
			var mu sync.Mutex
			now := time.Now()
			clock := func() time.Time { mu.Lock(); defer mu.Unlock(); return now }
			w, child := watchFixture(t, clock)
			a := armFixture()
			s := completeCounterReceipt(a, mode, 0)
			raw, err := Encode(s)
			var decoded Summary
			if err != nil || Decode(raw, &decoded) != nil || !decoded.Counters.Arithmetic() {
				t.Fatal("fixture not balanced strict metadata")
			}
			sendFixture(t, child, helloFixture(a))
			if _, err = w.AwaitHello(t.Context()); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { _, e := w.Arm(t.Context(), a); done <- e }()
			armRaw, err := ReadFrame(child)
			var gotArm Arm
			if err != nil || Decode(armRaw, &gotArm) != nil || gotArm != a {
				t.Fatal("actual ARM mismatch")
			}
			sendFixture(t, child, armedFixture(a))
			if err = <-done; err != nil {
				t.Fatal(err)
			}
			w.mu.Lock()
			armAt := w.armAt
			w.mu.Unlock()
			mu.Lock()
			now = armAt.Add(time.Duration(a.DurationMS) * time.Millisecond)
			mu.Unlock()
			sendFixture(t, child, s)
			got, err := w.Collect(t.Context(), a.OperationID)
			valid := mode == "valid_zero" || mode == "valid_vm_api_full" || mode == "valid_vm_denial" || mode == "valid_host_api_full" || mode == "valid_host_denial"
			if valid {
				if err != nil || got != s {
					t.Fatalf("valid Complete watch refused: %v", err)
				}
			} else {
				if err == nil {
					t.Fatal("watch returned contradictory Complete receipt")
				}
				if _, err = w.Collect(t.Context(), a.OperationID); err == nil {
					t.Fatal("invalid counter receipt revived")
				}
			}
		})
	}
}

func TestTimelyHelloSurvivesReviewBeyondThirtySeconds(t *testing.T) {
	var mu sync.Mutex
	now := time.Now()
	clock := func() time.Time { mu.Lock(); defer mu.Unlock(); return now }
	w, child := watchFixture(t, clock)
	a := armFixture()
	sendFixture(t, child, helloFixture(a))
	if _, err := w.AwaitHello(t.Context()); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	now = now.Add(31 * time.Second)
	mu.Unlock()
	if _, err := w.AwaitHello(t.Context()); err != nil {
		t.Fatal("timely retained HELLO lost during review", err)
	}
	done := make(chan error, 1)
	go func() { _, e := w.Arm(t.Context(), a); done <- e }()
	if _, err := ReadFrame(child); err != nil {
		t.Fatal(err)
	}
	sendFixture(t, child, armedFixture(a))
	if err := <-done; err != nil {
		t.Fatal("review delay prevented one ARM", err)
	}
}
func TestArmDurationBoundsRemainUnchanged(t *testing.T) {
	for _, n := range []uint32{999, 1000, 30000, 30001} {
		a := armFixture()
		a.DurationMS = n
		if a.Valid(a.Generation, a.Nonce, a.Candidate.MAC, a.Gateway) != (n >= 1000 && n <= 30000) {
			t.Fatal("ARM range changed", n)
		}
	}
}

func TestPrearmCapExclusiveStickyAndNeverExtended(t *testing.T) {
	for _, mode := range []string{"before", "exact", "after", "suspend", "wall-regression", "continuous-regression", "clock-error"} {
		t.Run(mode, func(t *testing.T) {
			var mu sync.Mutex
			r := ClockReading{WallNS: 1000000000000000000, ContinuousNS: 1000000000}
			bad := false
			c, e := newLaunchClock(func() (ClockReading, error) {
				mu.Lock()
				defer mu.Unlock()
				if bad {
					return ClockReading{}, ErrMetadata
				}
				return r, nil
			})
			if e != nil {
				t.Fatal(e)
			}
			mu.Lock()
			base := r
			r.WallNS += uint64(PrearmCap)
			r.ContinuousNS += uint64(PrearmCap)
			switch mode {
			case "before":
				r.WallNS--
				r.ContinuousNS--
			case "after":
				r.WallNS++
				r.ContinuousNS++
			case "suspend":
				r.WallNS = base.WallNS + 1
			case "wall-regression":
				r.WallNS = base.WallNS - 1
			case "continuous-regression":
				r.ContinuousNS = base.ContinuousNS - 1
			case "clock-error":
				bad = true
			}
			mu.Unlock()
			_, e = c.check(true)
			if mode == "before" {
				if e != nil {
					t.Fatal(e)
				}
				if c.anchor != base {
					t.Fatal("deadline anchor extended")
				}
			} else {
				if e == nil {
					t.Fatal("expiry/regression/error admitted")
				}
				mu.Lock()
				r = base
				bad = false
				mu.Unlock()
				if _, e = c.check(true); e == nil {
					t.Fatal("invalid clock revived")
				}
			}
		})
	}
}
func TestLaunchClockRejectsDeadlineOverflow(t *testing.T) {
	for _, r := range []ClockReading{{WallNS: 1<<63 - 1, ContinuousNS: 1}, {WallNS: 1, ContinuousNS: ^uint64(0)}} {
		if _, e := newLaunchClock(func() (ClockReading, error) { return r, nil }); e == nil {
			t.Fatal("unchecked cap overflow")
		}
	}
}

func TestPrearmIndependentContinuousExpiryWithoutCollector(t *testing.T) {
	var mu sync.Mutex
	r := ClockReading{WallNS: uint64(time.Now().UnixNano()), ContinuousNS: 1}
	c, e := newLaunchClock(func() (ClockReading, error) { mu.Lock(); defer mu.Unlock(); return r, nil })
	if e != nil {
		t.Fatal(e)
	}
	w, child := watchFixture(t, time.Now)
	w.Close()
	child.Close()
	fds, e := syscall.Socketpair(syscall.AF_UNIX, syscall.SOCK_STREAM, 0)
	if e != nil {
		t.Fatal(e)
	}
	for _, fd := range fds {
		syscall.CloseOnExec(fd)
		syscall.SetNonblock(fd, true)
	}
	a := armFixture()
	w = NewWatchAt(os.NewFile(uintptr(fds[0]), "cap-parent"), a.Generation, a.Nonce, a.Candidate.MAC, c)
	child = os.NewFile(uintptr(fds[1]), "cap-child")
	t.Cleanup(func() { w.Close(); child.Close() })
	sendFixture(t, child, helloFixture(a))
	if _, e = w.AwaitHello(t.Context()); e != nil {
		t.Fatal(e)
	}
	before, e := w.Observe()
	if e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 3; i++ {
		if got, e := w.Observe(); e != nil || got.Deadline != before.Deadline || got.Anchor != before.Anchor {
			t.Fatal("observation reset cap", e)
		}
	}
	mu.Lock()
	r.ContinuousNS += uint64(PrearmCap)
	mu.Unlock()
	select {
	case <-w.invalidDone:
	case <-time.After(time.Second):
		t.Fatal("no independent continuous cap wake after simulated suspend")
	}
	if _, e = w.ArmReceipt(t.Context(), a); e == nil {
		t.Fatal("expired cap rearmed")
	}
}
func TestHelloDecodePauseCannotAdmitLateFrame(t *testing.T) {
	var mu sync.Mutex
	now := time.Now()
	clock := func() time.Time { mu.Lock(); defer mu.Unlock(); return now }
	w, child := watchFixture(t, clock)
	pause, resume := make(chan struct{}), make(chan struct{})
	w.mu.Lock()
	w.afterHelloDecode = func() { close(pause); <-resume }
	w.mu.Unlock()
	a := armFixture()
	sendFixture(t, child, helloFixture(a))
	<-pause
	mu.Lock()
	now = now.Add(30 * time.Second)
	mu.Unlock()
	close(resume)
	if _, e := w.AwaitHello(t.Context()); e == nil {
		t.Fatal("postdecode late HELLO admitted")
	}
}
func TestPrearmCapQueuedARMExclusiveAndOneUse(t *testing.T) {
	for _, delta := range []time.Duration{-time.Nanosecond, 0, time.Nanosecond} {
		t.Run(delta.String(), func(t *testing.T) {
			var mu sync.Mutex
			now := time.Now()
			clock := func() time.Time { mu.Lock(); defer mu.Unlock(); return now }
			w, child := watchFixture(t, clock)
			a := armFixture()
			sendFixture(t, child, helloFixture(a))
			if _, e := w.AwaitHello(t.Context()); e != nil {
				t.Fatal(e)
			}
			mu.Lock()
			now = now.Add(PrearmCap + delta)
			mu.Unlock()
			if delta < 0 {
				done := make(chan error, 1)
				go func() { _, e := w.ArmReceipt(t.Context(), a); done <- e }()
				if _, e := ReadFrame(child); e != nil {
					t.Fatal(e)
				}
				sendFixture(t, child, armedFixture(a))
				if e := <-done; e != nil {
					t.Fatal("before cap refused", e)
				}
			} else {
				if _, e := w.ArmReceipt(t.Context(), a); e == nil {
					t.Fatal("exact/after cap admitted")
				}
				child.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
				var b [1]byte
				if n, _ := child.Read(b[:]); n != 0 {
					t.Fatal("expired ARM wrote bytes")
				}
			}
			if _, e := w.ArmReceipt(t.Context(), a); e == nil {
				t.Fatal("consumed/expired watch rearmed")
			}
		})
	}
}
func TestArmReceiptActualSendDeadlineDoesNotReset(t *testing.T) {
	var mu sync.Mutex
	now := time.Now()
	clock := func() time.Time { mu.Lock(); defer mu.Unlock(); return now }
	w, child := watchFixture(t, clock)
	a := armFixture()
	sendFixture(t, child, helloFixture(a))
	if _, e := w.AwaitHello(t.Context()); e != nil {
		t.Fatal(e)
	}
	done := make(chan struct {
		r ArmReceipt
		e error
	}, 1)
	go func() {
		r, e := w.ArmReceipt(t.Context(), a)
		done <- struct {
			r ArmReceipt
			e error
		}{r, e}
	}()
	if _, e := ReadFrame(child); e != nil {
		t.Fatal(e)
	}
	w.mu.Lock()
	sent, deadline := w.armReceipt.Sent, w.armReceipt.Deadline
	w.mu.Unlock()
	mu.Lock()
	now = now.Add(500 * time.Millisecond)
	mu.Unlock()
	sendFixture(t, child, armedFixture(a))
	got := <-done
	if got.e != nil || !got.r.Matches(a) || got.r.Sent != sent || got.r.Deadline != deadline {
		t.Fatal("receipt replaced actual send deadline", got.e)
	}
	if got.r.Current(deadline) {
		t.Fatal("exact expiry accepted")
	}
}

// This fixture holds the actual ARM caller after timely ARMED while the real
// reader accepts SUMMARY. Only clock readings are synthetic; frames and phase
// transitions use the retained nonblocking socket and production reader.
func timelySummaryPausedARMFixture(t *testing.T, phase string) (*Watch, ClockReading, ClockReading, func(ClockReading), func(), <-chan error) {
	t.Helper()
	var clockMu sync.Mutex
	r := ClockReading{WallNS: uint64(time.Now().UnixNano()), ContinuousNS: 1000000000}
	read := func() (ClockReading, error) { clockMu.Lock(); defer clockMu.Unlock(); return r, nil }
	set := func(next ClockReading) { clockMu.Lock(); r = next; clockMu.Unlock() }
	c, err := newLaunchClock(read)
	if err != nil {
		t.Fatal(err)
	}
	fds, err := syscall.Socketpair(syscall.AF_UNIX, syscall.SOCK_STREAM, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, fd := range fds {
		syscall.CloseOnExec(fd)
		syscall.SetNonblock(fd, true)
	}
	a := armFixture()
	w := watchAt(os.NewFile(uintptr(fds[0]), "i1-parent"), a.Generation, a.Nonce, a.Candidate.MAC, c, func() time.Time { got, _ := read(); return time.Unix(0, int64(got.WallNS)) })
	child := os.NewFile(uintptr(fds[1]), "i1-child")
	child.SetDeadline(time.Now().Add(5 * time.Second))
	paused, resume := make(chan struct{}), make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(resume) }) }
	t.Cleanup(func() { release(); w.Close(); child.Close() })
	w.afterArmedWait = func() { close(paused); <-resume }
	sendFixture(t, child, helloFixture(a))
	if _, err := w.AwaitHello(t.Context()); err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		receipt, err := w.ArmReceipt(t.Context(), a)
		if err == nil && !receipt.Matches(a) {
			err = ErrMetadata
		}
		result <- err
	}()
	if raw, err := ReadFrame(child); err != nil {
		t.Fatal(err)
	} else {
		var got Arm
		if Decode(raw, &got) != nil || got != a {
			t.Fatal("actual ARM mismatch")
		}
	}
	sendFixture(t, child, armedFixture(a))
	select {
	case <-paused:
	case <-time.After(2 * time.Second):
		t.Fatal("ARM waiter did not pause after actual ARMED")
	}
	w.mu.Lock()
	sent, deadline := w.armReceipt.Sent, w.armDeadline
	if w.phase != "armed" {
		t.Fatal("timely ARMED not admitted")
	}
	w.mu.Unlock()
	timely := ClockReading{WallNS: sent.WallNS + uint64(time.Duration(a.DurationMS)*time.Millisecond), ContinuousNS: sent.ContinuousNS + uint64(time.Duration(a.DurationMS)*time.Millisecond)}
	set(timely)
	sendFixture(t, child, summaryFixture(a))
	limit := time.Now().Add(2 * time.Second)
	for {
		w.mu.Lock()
		got := w.phase
		summary := w.summary
		w.mu.Unlock()
		if got == phase {
			if !summary.Matches(a, armedFixture(a)) {
				t.Fatal("timely SUMMARY not actually admitted")
			}
			break
		}
		if got == "invalid" || time.Now().After(limit) {
			t.Fatalf("wanted actual %s after SUMMARY, got %s", phase, got)
		}
		time.Sleep(time.Millisecond)
	}
	return w, timely, deadline, set, release, result
}

func TestDiagnosticWatchSummaryARMWaiterOriginalDeadline(t *testing.T) {
	for _, phase := range []string{"summary", "complete"} {
		for _, axis := range []string{"wall", "continuous"} {
			for _, edge := range []string{"before", "exact", "after"} {
				t.Run(phase+"/"+axis+"/"+edge, func(t *testing.T) {
					w, timely, deadline, set, release, result := timelySummaryPausedARMFixture(t, phase)
					next := timely
					bound := deadline.WallNS
					if axis == "continuous" {
						bound = deadline.ContinuousNS
					}
					if edge == "before" {
						bound--
					}
					if edge == "after" {
						bound++
					}
					if axis == "wall" {
						next.WallNS = bound
					} else {
						next.ContinuousNS = bound
					}
					set(next)
					release()
					select {
					case err := <-result:
						if (err == nil) != (edge == "before") {
							t.Fatalf("original %s ARM deadline %s in %s admitted=%t", axis, edge, phase, err == nil)
						}
					case <-time.After(time.Second):
						t.Fatal("ARM result remained blocked")
					}
					w.mu.Lock()
					gotPhase, gotDeadline := w.phase, w.armDeadline
					w.mu.Unlock()
					if gotDeadline != deadline {
						t.Fatal("original deadline changed")
					}
					if edge != "before" {
						if gotPhase != "invalid" {
							t.Fatal("late ARM result did not become sticky invalid")
						}
						set(timely)
						if _, err := w.armResult(); err == nil {
							t.Fatal("invalid ARM waiter revived")
						}
					}
				})
			}
		}
	}
}

func TestDiagnosticWatchCompletedCollectionRetainedWithoutLateARMWaiter(t *testing.T) {
	for _, axis := range []string{"wall", "continuous"} {
		t.Run(axis, func(t *testing.T) {
			w, timely, deadline, set, release, result := timelySummaryPausedARMFixture(t, "summary")
			// Complete the ARM caller on time, before reader completion and before
			// advancing either clock beyond the original ARM-result deadline.
			release()
			if err := <-result; err != nil {
				t.Fatal("timely ARM waiter refused", err)
			}
			select {
			case <-w.done:
			case <-time.After(2 * time.Second):
				t.Fatal("reader did not complete")
			}
			late := timely
			if axis == "wall" {
				late.WallNS = deadline.WallNS + 1
			} else {
				late.ContinuousNS = deadline.ContinuousNS + 1
			}
			set(late)
			w.mu.Lock()
			valid := w.validClockLocked()
			phase := w.phase
			w.mu.Unlock()
			if !valid || phase != "complete" {
				t.Fatal("general clock check changed completed-result contract")
			}
			a := armFixture()
			got, err := w.Collect(t.Context(), a.OperationID)
			if err != nil || !got.Matches(a, armedFixture(a)) {
				t.Fatal("retained completed SUMMARY was lost", err)
			}
		})
	}
}
