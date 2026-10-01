//go:build n1diagnostic && n1clipboarddiagnostic && !n1candidate

package coordinator

import (
	"bytes"
	"context"
	"errors"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/fixed"
	"io"
	"os"
	"os/exec"
	"sync"
)

type childResult struct {
	raw    []byte
	exit   int
	closed bool
	stderr bool
}
type retainedChild struct {
	cmd      *exec.Cmd
	out, err *os.File
	done     chan childResult
	cancel   context.CancelFunc
	once     sync.Once
	finished chan struct{}
	joinOnce sync.Once
	result   childResult
}

// Retains the exact child, bounds allocation before reading, drains both streams
// immediately, and actually waits/reaps. No PID lookup or process reconstruction.
func startChild(ctx context.Context, path string, args []string, input []byte, limit int, records func(io.Reader) ([]byte, error)) (*retainedChild, error) {
	return startChildEnvironment(ctx, path, args, fixed.Environment(false), input, limit, records)
}
func startChildEnvironment(ctx context.Context, path string, args, env []string, input []byte, limit int, records func(io.Reader) ([]byte, error)) (*retainedChild, error) {
	if ctx.Err() != nil || limit < 1 || limit > 32768 || len(input) > 16384 {
		return nil, ErrRefused
	}
	ctx, cancel := context.WithCancel(ctx)
	out, ow, e := os.Pipe()
	if e != nil {
		cancel()
		return nil, ErrRefused
	}
	er, ew, e := os.Pipe()
	if e != nil {
		out.Close()
		ow.Close()
		cancel()
		return nil, ErrRefused
	}
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Env = env
	cmd.Stdin = bytes.NewReader(input)
	cmd.Stdout = ow
	cmd.Stderr = ew
	if e = cmd.Start(); e != nil {
		out.Close()
		ow.Close()
		er.Close()
		ew.Close()
		cancel()
		return nil, ErrRefused
	}
	c := &retainedChild{cmd: cmd, out: out, err: er, done: make(chan childResult, 1), cancel: cancel, finished: make(chan struct{})}
	wc := errors.Join(ow.Close(), ew.Close())
	type drain struct {
		b []byte
		e error
	}
	och, ech := make(chan drain, 1), make(chan drain, 1)
	go func() {
		var b []byte
		var e error
		if records != nil {
			b, e = records(out)
		} else {
			b, e = io.ReadAll(io.LimitReader(out, int64(limit)+1))
		}
		if len(b) > limit {
			b = nil
			e = ErrRefused
		}
		if e != nil {
			cancel()
		}
		ce := out.Close()
		och <- drain{b, errors.Join(e, ce)}
	}()
	go func() {
		b, e := io.ReadAll(io.LimitReader(er, 4097))
		if len(b) > 4096 {
			b = nil
			e = ErrRefused
			cancel()
		}
		ce := er.Close()
		ech <- drain{b, errors.Join(e, ce)}
	}()
	// Cancellation also closes retained readers, so descendant-held pipes cannot
	// prevent bounded drain. Joining/reaping is still mandatory on refusal.
	go func() {
		select {
		case <-ctx.Done():
			c.once.Do(func() { out.Close(); er.Close() })
		case <-c.finished:
		}
	}()
	go func() {
		_ = cmd.Wait()
		o, s := <-och, <-ech
		r := childResult{o.b, cmd.ProcessState.ExitCode(), wc == nil && o.e == nil && s.e == nil, len(s.b) != 0}
		if ctx.Err() != nil {
			r.closed = false
		}
		c.done <- r
		close(c.finished)
		cancel()
	}()
	return c, nil
}
func (c *retainedChild) join() childResult {
	if c == nil {
		return childResult{exit: -1}
	}
	c.joinOnce.Do(func() { c.result = <-c.done })
	return c.result
}
func callChild(ctx context.Context, path string, args []string, input []byte, limit int) (childResult, error) {
	c, e := startChild(ctx, path, args, input, limit, nil)
	if e != nil {
		return childResult{exit: -1}, e
	}
	r := c.join()
	if r.exit != 0 || !r.closed || r.stderr {
		return r, ErrRefused
	}
	return r, nil
}
