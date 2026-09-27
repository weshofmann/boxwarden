package workspacex

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"syscall"
	"time"
)

var errExportBuilderLifetimeUnproven = errors.New("inspector builder lifetime is unproven")

// The fixed builder and its tools must not detach. Group signaling and direct
// child Wait4 share one mutex: a reaped/reused PGID never becomes kill authority.
// Pipe EOF proves inherited writers closed; Wait4 cannot reap grandchildren.
func runExportBuilderProcess(ctx context.Context, script string, args, env []string) (string, string, error) {
	if err := ctx.Err(); err != nil {
		return "", "", err
	}
	null, err := os.Open(os.DevNull)
	if err != nil {
		return "", "", err
	}
	defer null.Close()
	outRead, outWrite, err := os.Pipe()
	if err != nil {
		return "", "", err
	}
	defer outRead.Close()
	defer outWrite.Close()
	errRead, errWrite, err := os.Pipe()
	if err != nil {
		return "", "", err
	}
	defer errRead.Close()
	defer errWrite.Close()
	process, err := os.StartProcess("/bin/bash", append([]string{"/bin/bash", script}, args...), &os.ProcAttr{Env: env, Files: []*os.File{null, outWrite, errWrite}, Sys: &syscall.SysProcAttr{Setpgid: true}})
	if err != nil {
		return "", "", err
	}
	_ = outWrite.Close()
	_ = errWrite.Close()
	stdout, stderr := &boundedBuildLog{limit: inspectorBuildLogLimit}, &boundedBuildLog{limit: inspectorBuildLogLimit}
	drained := make(chan error, 1)
	go func() {
		results := make(chan error, 2)
		go func() { _, err := io.Copy(stdout, outRead); results <- err }()
		go func() { _, err := io.Copy(stderr, errRead); results <- err }()
		drained <- errors.Join(<-results, <-results)
	}()
	owned := &exportBuilderProcess{process: process, done: make(chan struct{}), signal: syscall.Kill, poll: pollExportBuilderWait}
	go owned.reap()
	var result error
	select {
	case <-owned.done:
		result = owned.waitErr
	case <-ctx.Done():
		result = errors.Join(ctx.Err(), owned.stop())
		select {
		case <-owned.done:
			result = errors.Join(result, owned.waitErr)
		case <-time.After(5 * time.Second):
			return "", "", errors.Join(result, errExportBuilderLifetimeUnproven)
		}
	}
	select {
	case drainErr := <-drained:
		if drainErr != nil {
			result = errors.Join(result, errExportBuilderLifetimeUnproven, drainErr)
		}
	case <-time.After(5 * time.Second):
		return "", "", errors.Join(result, errExportBuilderLifetimeUnproven)
	}
	if stdout.overflow || stderr.overflow {
		result = errors.Join(result, errors.New("inspector builder output exceeded bound"))
	}
	return stdout.String(), stderr.String(), errors.Join(result, ctx.Err())
}

type exportBuilderProcess struct {
	process       *os.Process
	done          chan struct{}
	mu            sync.Mutex
	reaped        bool
	authorityLost bool
	waitErr       error
	signal        func(int, syscall.Signal) error
	poll          func(int) (int, syscall.WaitStatus, error)
	release       func() error
}

func (p *exportBuilderProcess) stop() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.reaped {
		return nil
	}
	if p.authorityLost {
		return errExportBuilderLifetimeUnproven
	}
	if err := p.signal(-p.process.Pid, syscall.SIGKILL); err != nil {
		return fmt.Errorf("kill owned script process group: %w", err)
	}
	return nil
}

func pollExportBuilderWait(child int) (int, syscall.WaitStatus, error) {
	var status syscall.WaitStatus
	pid, err := syscall.Wait4(child, &status, syscall.WNOHANG, nil)
	return pid, status, err
}

func (p *exportBuilderProcess) reap() {
	for {
		p.mu.Lock()
		pid, status, err := p.poll(p.process.Pid)
		if errors.Is(err, syscall.EINTR) {
			p.mu.Unlock()
			continue
		}
		if pid == 0 && err == nil {
			p.mu.Unlock()
			time.Sleep(25 * time.Millisecond)
			continue
		}
		if err != nil || pid != p.process.Pid {
			p.waitErr = fmt.Errorf("%w: wait4 returned pid %d: %v", errExportBuilderLifetimeUnproven, pid, err)
			p.authorityLost = true
			close(p.done)
			p.mu.Unlock()
			return
		} else if status.Signaled() {
			p.waitErr = fmt.Errorf("script terminated by signal %s", status.Signal())
		} else if !status.Exited() || status.ExitStatus() != 0 {
			p.waitErr = fmt.Errorf("script exited with status %#x", status)
		}
		p.reaped = true
		release := p.release
		if release == nil {
			release = p.process.Release
		}
		p.waitErr = errors.Join(p.waitErr, release())
		close(p.done)
		p.mu.Unlock()
		return
	}
}
