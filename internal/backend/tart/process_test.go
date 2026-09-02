package tart

import (
	"context"
	"os"
	"os/exec"
	"sync"
	"testing"
)

func TestNewOwnedCommandCreatesPrivateProcessGroup(t *testing.T) {
	command := newOwnedCommand(ProcessSpec{Path: "/opt/qualified/tart", Args: []string{"run"}, Env: []string{"PATH=/qualified"}, Dir: "/private/runtime/generation"})
	if command.SysProcAttr == nil || !command.SysProcAttr.Setpgid {
		t.Fatalf("command process attributes = %#v, want private process group", command.SysProcAttr)
	}
	if command.Path != "/opt/qualified/tart" || command.Dir != "/private/runtime/generation" || !sameStrings(command.Args[1:], []string{"run"}) {
		t.Fatalf("owned command = %#v, want unchanged direct process spec", command)
	}
}

func TestOwnedHandleSignalsExactGroupOnceUnderConcurrentStops(t *testing.T) {
	var mutex sync.Mutex
	groups := []int{}
	handle := &osProcessHandle{
		command: &exec.Cmd{Process: &os.Process{Pid: 4242}},
		done:    make(chan struct{}),
		signalGroup: func(group int) error {
			mutex.Lock()
			defer mutex.Unlock()
			groups = append(groups, group)
			return nil
		},
	}

	var workers sync.WaitGroup
	for worker := 0; worker < 16; worker++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			if err := handle.Stop(context.Background()); err != nil {
				t.Errorf("Stop() error = %v", err)
			}
		}()
	}
	workers.Wait()
	if got, want := groups, []int{-4242}; len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("signaled groups = %#v, want exact owned group %#v", got, want)
	}
}

func TestOwnedHandleDoesNotSignalAfterCompletionAndWaitIsRepeatable(t *testing.T) {
	done := make(chan struct{})
	close(done)
	groups := 0
	handle := &osProcessHandle{
		command: &exec.Cmd{Process: &os.Process{Pid: 4242}},
		done:    done,
		signalGroup: func(int) error {
			groups++
			return nil
		},
	}
	if err := handle.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() after completion error = %v", err)
	}
	if err := handle.Wait(context.Background()); err != nil {
		t.Fatalf("first Wait() error = %v", err)
	}
	if err := handle.Wait(context.Background()); err != nil {
		t.Fatalf("second Wait() error = %v", err)
	}
	if groups != 0 {
		t.Fatalf("completed handle signaled %d groups, want none", groups)
	}
}
