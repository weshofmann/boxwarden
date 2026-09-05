package tart

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"sync"
	"testing"
)

func TestOwnedHandleStopsOnlyItsProcessGroup(t *testing.T) {
	var mu sync.Mutex
	var groups []int
	handle := &osProcessHandle{
		command: &exec.Cmd{Process: &os.Process{Pid: 4242}},
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

func TestOwnedHandleWaitCancellationStillReapsOnlyOnce(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	var mu sync.Mutex
	waits := 0
	handle := &osProcessHandle{
		command: &exec.Cmd{},
		done:    make(chan struct{}),
		waitCommand: func() error {
			mu.Lock()
			waits++
			mu.Unlock()
			close(entered)
			<-release
			return nil
		},
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
