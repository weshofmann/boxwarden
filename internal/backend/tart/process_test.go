package tart

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

func TestOwnedProcessChildObservesExactDiskArgv(t *testing.T) {
	if !supportsOwnedProcessGroups() {
		t.Skip("owned Tart process groups are Darwin-only")
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "argv.json")
	want := []string{"-test.run=^TestOwnedProcessArgvChild$", "--", "run", "--disk", "/private/state with spaces/volumes/00112233-4455-4677-8899-aabbccddeeff.raw", "boxwarden-work-dev"}
	handle, err := (osProcessStarter{}).start(context.Background(), processSpec{
		path: executable, args: want, env: []string{"BOXWARDEN_TEST_ARGV_OUTPUT=" + output}, dir: "/",
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := handle.Wait(ctx); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	var observed []string
	if err := json.Unmarshal(raw, &observed); err != nil {
		t.Fatal(err)
	}
	if !sameLifecycleStrings(observed, want) {
		t.Fatalf("child argv = %#v, want exact elements %#v", observed, want)
	}
}

func TestOwnedProcessArgvChild(t *testing.T) {
	output := os.Getenv("BOXWARDEN_TEST_ARGV_OUTPUT")
	if output == "" {
		return
	}
	raw, err := json.Marshal(os.Args[1:])
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(output, raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestOwnedProcessUsesRealDirectChildWait(t *testing.T) {
	if !supportsOwnedProcessGroups() {
		t.Skip("owned Tart process groups are Darwin-only")
	}
	handle, err := (osProcessStarter{}).start(context.Background(), processSpec{
		path: "/bin/sh", args: []string{"-c", "exit 7"}, env: []string{"PATH=/usr/bin:/bin"}, dir: "/",
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := handle.Wait(ctx); err == nil || !strings.Contains(err.Error(), "status 7") {
		t.Fatalf("direct child Wait() = %v, want exact nonzero exit", err)
	}
	if err := handle.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() after exact reap = %v", err)
	}
}

func TestRetainedChildLivenessEndsOnExactReapOrLostAuthority(t *testing.T) {
	for _, lost := range []bool{false, true} {
		handle := &osProcessHandle{process: &os.Process{Pid: 4242}, done: make(chan struct{})}
		if !handle.RetainedChildLive() {
			t.Fatal("newly retained child was not live")
		}
		handle.stopMu.Lock()
		if lost {
			handle.authorityLost = true
		} else {
			handle.reaped = true
		}
		close(handle.done)
		handle.stopMu.Unlock()
		if handle.RetainedChildLive() {
			t.Fatalf("retained child reported live after reap/lost authority (lost=%t)", lost)
		}
	}
}

func TestAmbiguousOwnedWaitPreservesScratchAndRefusesLateSignal(t *testing.T) {
	for name, poll := range map[string]func(int) (int, syscall.WaitStatus, error){
		"ECHILD":    func(int) (int, syscall.WaitStatus, error) { return -1, 0, syscall.ECHILD },
		"other pid": func(int) (int, syscall.WaitStatus, error) { return 5555, 0, nil },
	} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.Chmod(root, 0o700); err != nil {
				t.Fatal(err)
			}
			scratch, info, err := createScratch(root)
			if err != nil {
				t.Fatal(err)
			}
			signals := 0
			owned := &osProcessHandle{
				process: &os.Process{Pid: 4242}, done: make(chan struct{}), pollWait: poll,
				signalGroup:    func(int) error { signals++; return nil },
				releaseProcess: func() error { t.Fatal("ambiguous wait released process"); return nil },
			}
			handle := &scratchHandle{Handle: owned, path: scratch, info: info}
			if err := handle.Wait(context.Background()); !errors.Is(err, ErrReapUnproven) {
				t.Fatalf("Wait() = %v, want unproven reap", err)
			}
			if _, err := os.Lstat(scratch); err != nil {
				t.Fatalf("unproven reap removed scratch: %v", err)
			}
			if err := handle.Stop(context.Background()); !errors.Is(err, ErrReapUnproven) || signals != 0 {
				t.Fatalf("Stop() after lost wait authority = %v; signals=%d", err, signals)
			}
			if err := handle.RequestStop(context.Background()); !errors.Is(err, ErrReapUnproven) {
				t.Fatalf("RequestStop() after lost wait authority = %v", err)
			}
		})
	}
}

func TestOwnedHandleStopsOnlyItsProcessGroup(t *testing.T) {
	var mu sync.Mutex
	var groups []int
	handle := &osProcessHandle{
		process: &os.Process{Pid: 4242},
		done:    make(chan struct{}),
		signalGroup: func(group int) error {
			mu.Lock()
			defer mu.Unlock()
			groups = append(groups, group)
			return nil
		},
	}
	var workers sync.WaitGroup
	for range 16 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			if err := handle.Stop(context.Background()); err != nil {
				t.Errorf("Stop() error = %v", err)
			}
		}()
	}
	workers.Wait()
	if got, want := groups, []int{-4242}; !sameInts(got, want) {
		t.Fatalf("signaled groups = %#v, want exact owned group %#v", got, want)
	}
}

func TestOwnedHandleRequestsGuestShutdownOnExactProcess(t *testing.T) {
	var targets []int
	handle := &osProcessHandle{
		process: &os.Process{Pid: 4242}, done: make(chan struct{}),
		requestStop: func(pid int) error { targets = append(targets, pid); return nil },
		signalGroup: func(int) error { t.Fatal("guest request used force-stop group"); return nil },
	}
	if err := handle.RequestStop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := handle.RequestStop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got, want := targets, []int{4242}; !sameInts(got, want) {
		t.Fatalf("requested guest stop targets = %v, want %v", got, want)
	}
}

func TestOwnedHandleRetriesFailedGuestShutdownRequest(t *testing.T) {
	attempts := 0
	want := errors.New("transient request failure")
	handle := &osProcessHandle{
		process: &os.Process{Pid: 4242}, done: make(chan struct{}),
		requestStop: func(pid int) error {
			if pid != 4242 {
				t.Fatalf("request targeted process %d", pid)
			}
			attempts++
			if attempts == 1 {
				return want
			}
			return nil
		},
	}
	if err := handle.RequestStop(context.Background()); !errors.Is(err, want) {
		t.Fatalf("first guest stop request = %v", err)
	}
	if err := handle.RequestStop(context.Background()); err != nil || attempts != 2 {
		t.Fatalf("retried guest stop request = %v; attempts=%d", err, attempts)
	}
}

func TestOwnedHandleRetriesFailedSignalOnExactProcessGroup(t *testing.T) {
	attempts := 0
	want := errors.New("transient signal failure")
	handle := &osProcessHandle{
		process: &os.Process{Pid: 4242},
		done:    make(chan struct{}),
		signalGroup: func(group int) error {
			if group != -4242 {
				t.Fatalf("signaled group %d", group)
			}
			attempts++
			if attempts == 1 {
				return want
			}
			return nil
		},
	}
	if err := handle.Stop(context.Background()); !errors.Is(err, want) {
		t.Fatalf("first Stop = %v", err)
	}
	if err := handle.Stop(context.Background()); err != nil {
		t.Fatalf("retry Stop = %v", err)
	}
	if err := handle.Stop(context.Background()); err != nil || attempts != 2 {
		t.Fatalf("idempotent Stop = %v; attempts=%d", err, attempts)
	}
}

func TestOwnedHandleWaitCancellationStillReapsOnlyOnce(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	var mu sync.Mutex
	waits := 0
	handle := &osProcessHandle{
		process: &os.Process{Pid: 4242},
		done:    make(chan struct{}),
		pollWait: func(int) (int, syscall.WaitStatus, error) {
			mu.Lock()
			waits++
			mu.Unlock()
			close(entered)
			<-release
			return 4242, 0, nil
		},
		releaseProcess: func() error { return nil },
	}
	ctx, cancel := context.WithCancel(context.Background())
	finished := make(chan error, 1)
	go func() { finished <- handle.Wait(ctx) }()
	<-entered
	cancel()
	if err := <-finished; !errors.Is(err, context.Canceled) {
		t.Fatalf("Wait() after cancellation = %v, want context cancellation", err)
	}
	close(release)
	if err := handle.Wait(context.Background()); err != nil {
		t.Fatalf("Wait() after reaping error = %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if waits != 1 {
		t.Fatalf("wait command calls = %d, want exactly one reap", waits)
	}
}

func TestOwnedWaitAndStopSerializeReapWithGroupSignal(t *testing.T) {
	entered := make(chan struct{})
	releasePoll := make(chan struct{})
	signals := 0
	handle := &osProcessHandle{
		process: &os.Process{Pid: 4242}, done: make(chan struct{}),
		pollWait: func(int) (int, syscall.WaitStatus, error) {
			close(entered)
			<-releasePoll
			return 4242, 0, nil
		},
		releaseProcess: func() error { return nil },
		signalGroup:    func(int) error { signals++; return nil },
	}
	waited := make(chan error, 1)
	go func() { waited <- handle.Wait(context.Background()) }()
	<-entered
	stopped := make(chan error, 1)
	go func() { stopped <- handle.Stop(context.Background()) }()
	select {
	case <-stopped:
		t.Fatal("Stop raced past in-progress reap")
	case <-time.After(20 * time.Millisecond):
	}
	close(releasePoll)
	if err := <-waited; err != nil {
		t.Fatal(err)
	}
	if err := <-stopped; err != nil || signals != 0 {
		t.Fatalf("Stop after reap = %v; signals=%d", err, signals)
	}
}

func sameInts(got, want []int) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
