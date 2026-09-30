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
