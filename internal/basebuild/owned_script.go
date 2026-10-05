package basebuild

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"syscall"
	"time"

	"github.com/weshofmann/boxwarden/internal/execx"
)

// OwnedScriptRunner runs fixed guest-definition scripts and owns their process
// groups until the direct child has been reaped.
type OwnedScriptRunner interface {
	RunOwned(context.Context, execx.Command) (execx.Result, error)
}

var ErrScriptReapUnproven = errors.New("owned script reap is unproven")

// Output receives merged actual stdout/stderr. No output is retained by the runner.
// Output is bounded to 8 MiB and a write failure stops the owned process group.
type OSOwnedScriptRunner struct{ Output io.Writer }

func (runner OSOwnedScriptRunner) RunOwned(ctx context.Context, command execx.Command) (execx.Result, error) {
	if err := ctx.Err(); err != nil {
		return execx.Result{}, err
	}
	if !absoluteClean(command.Path) || len(command.Stdin) != 0 {
		return execx.Result{}, errors.New("owned script requires an absolute path and no stdin")
	}
	devNull, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		return execx.Result{}, err
	}
	defer devNull.Close()
	var outputRead, outputWrite *os.File
	outputFile := devNull
	if runner.Output != nil {
		outputRead, outputWrite, err = os.Pipe()
		if err != nil {
			return execx.Result{}, err
		}
		defer outputRead.Close()
		defer outputWrite.Close()
		outputFile = outputWrite
	}
	process, err := os.StartProcess(command.Path, append([]string{command.Path}, command.Args...), &os.ProcAttr{
		Env:   append([]string(nil), command.Env...),
		Files: []*os.File{devNull, outputFile, outputFile},
		Sys:   &syscall.SysProcAttr{Setpgid: true},
	})
	if err != nil {
		return execx.Result{}, fmt.Errorf("start owned script: %w", err)
	}
	owned := &ownedScriptProcess{process: process, done: make(chan struct{}), signal: syscall.Kill, poll: pollScriptWait}

	go owned.reap()
	var drained chan error
	if outputRead != nil {
		_ = outputWrite.Close()
		drained = make(chan error, 1)
		go func() {
			limited := &io.LimitedReader{R: outputRead, N: 8 << 20}
			_, copyErr := io.Copy(runner.Output, limited)
			if copyErr == nil && limited.N == 0 {
				var extra [1]byte
				n, readErr := outputRead.Read(extra[:])
				if n != 0 {
					copyErr = errors.New("owned script output exceeds 8 MiB")
				} else if readErr != io.EOF {
					copyErr = readErr
				}
			}
			drained <- copyErr
		}()
	}
	var outputErr error
	reapTimeout := 30 * time.Second
	if runner.Output != nil {
		reapTimeout = 5 * time.Second
	}
	wait := func() error {
		select {
		case <-owned.done:
			return owned.waitErr
		case <-ctx.Done():
			stopErr := owned.stop()
			select {
			case <-owned.done:
				return errors.Join(ctx.Err(), stopErr, owned.waitErr)
			case <-time.After(reapTimeout):
				return errors.Join(ctx.Err(), stopErr, ErrScriptReapUnproven)
			}
		}
	}
	var waitErr error
	select {
	case <-owned.done:
		waitErr = owned.waitErr
	case <-ctx.Done():
		waitErr = wait()
	case outputErr = <-drained:
		drained = nil
		if outputErr != nil {
			stopErr := owned.stop()
			select {
			case <-owned.done:
				waitErr = errors.Join(stopErr, owned.waitErr)
			case <-time.After(reapTimeout):
				waitErr = errors.Join(stopErr, ErrScriptReapUnproven)
			}
		} else {
			waitErr = wait()
		}
	}
	if drained != nil {
		select {
		case outputErr = <-drained:
		case <-time.After(5 * time.Second):
			outputErr = errors.New("owned script output drain is unproven")
		}
	}
	return execx.Result{}, errors.Join(waitErr, outputErr, ctx.Err())
}

type ownedScriptProcess struct {
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

func (p *ownedScriptProcess) stop() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.reaped {
		return nil
	}
	if p.authorityLost {
		return ErrScriptReapUnproven
	}
	if err := p.signal(-p.process.Pid, syscall.SIGKILL); err != nil {
		return fmt.Errorf("kill owned script process group: %w", err)
	}
	return nil
}

func pollScriptWait(child int) (int, syscall.WaitStatus, error) {
	var status syscall.WaitStatus
	pid, err := syscall.Wait4(child, &status, syscall.WNOHANG, nil)
	return pid, status, err
}

func (p *ownedScriptProcess) reap() {
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
			p.waitErr = fmt.Errorf("%w: wait4 returned pid %d: %v", ErrScriptReapUnproven, pid, err)
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
