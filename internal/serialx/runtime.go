// Package serialx owns one private Tart serial PTY and its bootstrap/drain pump.
package serialx

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"unicode"

	"github.com/weshofmann/boxwarden/internal/guestproto"
)

var ErrPoisoned = errors.New("serial transport is poisoned")

// Runtime owns the PTY, its only reader, and a newly created serial subtree.
// The supervisor owns the enclosing generation and must close this runtime
// after Tart is stopped/reaped. No caller receives the master or a writer.
type Runtime struct {
	mu                              sync.Mutex
	stream                          io.ReadWriteCloser
	slave                           *os.File
	directory, endpoint, generation string
	attempted, resolved             bool
	parser                          *bootstrapParser
	result                          guestproto.SerialResult
	err                             error
	ready, pumpDone                 chan struct{}
	stopOnce, closeOnce             sync.Once
	closeErr                        error
}

// CreateRuntime admits an existing supervisor-owned generation, exclusively
// creates serial/, and starts draining immediately. ctx bounds creation only;
// the retained owner must call Close to end this runtime's lifetime.
func CreateRuntime(ctx context.Context, generationDirectory string) (*Runtime, error) {
	return createRuntime(ctx, generationDirectory, allocatePTY)
}

func createRuntime(ctx context.Context, generationDirectory string, allocate func() (*os.File, *os.File, error)) (*Runtime, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if generationDirectory == "/" || !filepath.IsAbs(generationDirectory) || filepath.Clean(generationDirectory) != generationDirectory || strings.IndexFunc(generationDirectory, unicode.IsControl) >= 0 {
		return nil, fmt.Errorf("generation directory must be canonical and absolute")
	}
	info, err := os.Lstat(generationDirectory)
	if err != nil {
		return nil, fmt.Errorf("inspect generation: %w", err)
	}
	if !info.IsDir() || info.Mode().Perm() != 0o700 || !ownedByCurrentUser(info) {
		return nil, fmt.Errorf("generation directory is not owner-private")
	}
	directory := filepath.Join(generationDirectory, "serial")
	// Mkdir is the admission boundary. Never adopt even an empty existing tree.
	if err := os.Mkdir(directory, 0o700); err != nil {
		return nil, fmt.Errorf("create exclusive serial subtree: %w", err)
	}
	master, slave, err := allocate()
	if err != nil {
		_ = os.Remove(directory)
		return nil, fmt.Errorf("allocate serial PTY: %w", err)
	}
	endpoint := filepath.Join(directory, "tart-serial")
	cleanup := func(err error) (*Runtime, error) {
		_ = master.Close()
		_ = slave.Close()
		_ = os.Remove(directory)
		return nil, err
	}
	info, err = slave.Stat()
	if err != nil || info.Mode()&os.ModeCharDevice == 0 || info.Mode().Perm() != 0o600 || !ownedByCurrentUser(info) {
		return cleanup(fmt.Errorf("serial slave is not an owner-private character device"))
	}
	if err := os.Symlink(slave.Name(), endpoint); err != nil {
		return cleanup(fmt.Errorf("publish Tart endpoint: %w", err))
	}
	r := newRuntime(master, filepath.Base(generationDirectory))
	r.slave, r.directory, r.endpoint = slave, directory, endpoint
	return r, nil
}

func newRuntime(stream io.ReadWriteCloser, generation string) *Runtime {
	r := &Runtime{stream: stream, generation: generation, ready: make(chan struct{}), pumpDone: make(chan struct{})}
	go r.pump()
	return r
}

func (r *Runtime) TartSlave() string { return r.endpoint }

// Err reports terminal transport failure, including failure after bootstrap
// during continuous drain. No serial recovery or second bootstrap is allowed.
func (r *Runtime) Err() error { r.mu.Lock(); defer r.mu.Unlock(); return r.err }

func (r *Runtime) fail(err error) {
	r.mu.Lock()
	if r.err == nil {
		r.err = fmt.Errorf("%w: %w", ErrPoisoned, err)
	}
	r.parser = nil
	if !r.resolved {
		r.resolved = true
		close(r.ready)
	}
	r.mu.Unlock()
	r.stopOnce.Do(func() { _ = r.stream.Close() })
}

func (r *Runtime) pump() {
	defer close(r.pumpDone)
	var chunk [4096]byte
	for {
		n, readErr := r.stream.Read(chunk[:])
		r.mu.Lock()
		var parseErr error
		if n > 0 && r.parser != nil {
			result, complete, err := r.parser.feed(chunk[:n])
			parseErr = err
			if err == nil && complete {
				r.result = result
				// This transition is permanent, including the remainder of this
				// read. Later bytes have no authority and are simply discarded.
				r.parser = nil
				r.resolved = true
				close(r.ready)
			}
		}
		r.mu.Unlock()
		if parseErr != nil {
			r.fail(parseErr)
			return
		}
		if readErr != nil {
			r.fail(fmt.Errorf("read serial PTY: %w", readErr))
			return
		}
	}
}

// Close interrupts blocked I/O, joins the sole reader, and removes only this
// runtime's fixed endpoint and empty serial directory. It never removes the
// enclosing generation or recursively deletes unexpected contents.
func (r *Runtime) Close() error {
	r.closeOnce.Do(func() {
		r.fail(errors.New("serial runtime closed"))
		if r.slave != nil {
			r.closeErr = r.slave.Close()
		}
		<-r.pumpDone
		if r.endpoint != "" {
			target, err := os.Readlink(r.endpoint)
			if err == nil && target != r.slave.Name() {
				err = fmt.Errorf("serial endpoint changed")
			}
			if err == nil {
				err = os.Remove(r.endpoint)
			}
			r.closeErr = errors.Join(r.closeErr, err)
		}
		if r.directory != "" {
			r.closeErr = errors.Join(r.closeErr, os.Remove(r.directory))
		}
	})
	return r.closeErr
}

func ownedByCurrentUser(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && int(stat.Uid) == os.Getuid()
}
