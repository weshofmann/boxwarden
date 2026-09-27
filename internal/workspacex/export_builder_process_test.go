package workspacex

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestExportBuilderDoesNotSignalAfterSerializedReap(t *testing.T) {
	process, err := os.FindProcess(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	entered, continuePoll := make(chan struct{}), make(chan struct{})
	signaled := make(chan struct{}, 1)
	owned := &exportBuilderProcess{
		process: process,
		done:    make(chan struct{}),
		poll: func(child int) (int, syscall.WaitStatus, error) {
			close(entered)
			<-continuePoll
			return child, 0, nil
		},
		signal:  func(int, syscall.Signal) error { signaled <- struct{}{}; return nil },
		release: func() error { return nil },
	}
	go owned.reap()
	<-entered
	stopped := make(chan error, 1)
	go func() { stopped <- owned.stop() }()
	close(continuePoll)
	if err := <-stopped; err != nil {
		t.Fatal(err)
	}
	<-owned.done
	select {
	case <-signaled:
		t.Fatal("signaled reaped process group")
	default:
	}
}
func TestExportBuilderLostWaitAuthorityNeverSignals(t *testing.T) {
	process, err := os.FindProcess(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	owned := &exportBuilderProcess{
		process: process,
		done:    make(chan struct{}),
		poll:    func(int) (int, syscall.WaitStatus, error) { return -1, 0, syscall.ECHILD },
		signal:  func(int, syscall.Signal) error { t.Error("signaled without wait authority"); return nil },
	}
	owned.reap()
	if !errors.Is(owned.waitErr, errExportBuilderLifetimeUnproven) || !errors.Is(owned.stop(), errExportBuilderLifetimeUnproven) {
		t.Fatal("lost wait authority not retained")
	}
}

func TestExportBuilderDrainsAndBoundsBothLogs(t *testing.T) {
	root := privateRoot(t)
	script := filepath.Join(root, "builder.sh")
	if err := os.WriteFile(script, []byte("#!/bin/bash\nprintf '%020000d' 0\nprintf '%020000d' 0 >&2\n"), 0700); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, err := runExportBuilderProcess(context.Background(), script, nil, []string{"PATH=/usr/bin:/bin"})
	if err == nil || !strings.Contains(err.Error(), "output exceeded bound") || errors.Is(err, errExportBuilderLifetimeUnproven) {
		t.Fatalf("overflow result: %v", err)
	}
	if len(stdout) != inspectorBuildLogLimit || len(stderr) != inspectorBuildLogLimit {
		t.Fatalf("logs not bounded: stdout=%d stderr=%d", len(stdout), len(stderr))
	}
}
