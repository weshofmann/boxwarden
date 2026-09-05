package tart

import (
	"context"
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
