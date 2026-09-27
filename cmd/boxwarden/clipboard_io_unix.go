//go:build darwin || linux

package main

import (
	"context"
	"errors"
	"os"
	"sync"
	"syscall"
)

type clipboardInput struct {
	file   *os.File
	source *os.File
	once   sync.Once
	err    error
	owned  *clipboardDescriptor
}

// Dup shares status flags with the caller. SyscallConn preserves the original
// flags while obtaining the descriptor; os.File.Fd may itself clear nonblock.
func newClipboardInput(source *os.File) (*clipboardInput, error) {
	raw, err := source.SyscallConn()
	if err != nil {
		return nil, err
	}
	descriptor := -1
	originalNonblock := false
	var descriptorErr error
	err = raw.Control(func(fd uintptr) {
		flags, _, errno := syscall.Syscall(syscall.SYS_FCNTL, fd, syscall.F_GETFL, 0)
		if errno != 0 {
			descriptorErr = errno
			return
		}
		originalNonblock = flags&syscall.O_NONBLOCK != 0
		descriptor, descriptorErr = syscall.Dup(int(fd))
	})
	if err != nil || descriptorErr != nil {
		return nil, errors.Join(err, descriptorErr)
	}
	syscall.CloseOnExec(descriptor)
	restoration, err := syscall.Dup(descriptor)
	if err != nil {
		syscall.Close(descriptor)
		return nil, err
	}
	syscall.CloseOnExec(restoration)
	owned := &clipboardDescriptor{descriptor: descriptor, restoration: restoration, originalNonblock: originalNonblock}
	if err := syscall.SetNonblock(descriptor, true); err != nil {
		return nil, errors.Join(err, owned.Close())
	}
	owned.file = os.NewFile(uintptr(descriptor), "clipboard-input")
	if owned.file == nil {
		return nil, errors.Join(os.ErrInvalid, owned.Close())
	}
	return &clipboardInput{file: owned.file, owned: owned}, nil
}

type clipboardDescriptor struct {
	file             *os.File
	descriptor       int
	restoration      int
	closeIO          func() error
	originalNonblock bool
	closeOnce        sync.Once
	closeErr         error
}

// Quiesce owned IO before restoring shared status flags. A separate raw
// duplicate retains the file description while Close evicts/join poller users;
// restoring blocking earlier could strand a retrying read/write syscall.
func (d *clipboardDescriptor) Close() error {
	d.closeOnce.Do(func() {
		if d.file == nil {
			d.closeErr = errors.Join(syscall.SetNonblock(d.restoration, d.originalNonblock), syscall.Close(d.descriptor), syscall.Close(d.restoration))
			return
		}
		closeIO := d.closeIO
		if closeIO == nil {
			closeIO = d.file.Close
		}
		ioErr := closeIO()
		d.closeErr = errors.Join(ioErr, syscall.SetNonblock(d.restoration, d.originalNonblock), syscall.Close(d.restoration))
	})
	return d.closeErr
}

func (r *clipboardInput) initialize() error {
	r.once.Do(func() {
		if r.file == nil {
			value, err := newClipboardInput(r.source)
			r.err = err
			if err == nil {
				r.file, r.owned = value.file, value.owned
			}
		}
	})
	return r.err
}
func (r *clipboardInput) Close() error {
	// Serialize with lazy initialization, including a Close before first use.
	r.once.Do(func() {
		if r.owned == nil {
			r.err = os.ErrClosed
		}
	})
	if r.owned == nil {
		return nil
	}
	return r.owned.Close()
}
func (r *clipboardInput) Read(p []byte) (int, error) {
	if err := r.initialize(); err != nil {
		return 0, err
	}
	return r.file.Read(p)
}
func (r *clipboardInput) ReadContext(ctx context.Context, p []byte) (int, error) {
	if ctx.Err() != nil {
		return 0, ctx.Err()
	}
	if err := r.initialize(); err != nil {
		return 0, err
	}
	stop := context.AfterFunc(ctx, func() { r.Close() })
	defer stop()
	n, err := r.file.Read(p)
	if ctx.Err() != nil {
		r.Close()
		return n, ctx.Err()
	}
	return n, err
}

// clipboardOutput owns a duplicate only when an explicit paste reaches commit.
// Closing it on cancellation wakes a blocked pipe write without closing stdout.
type clipboardOutput struct {
	source *os.File
	file   *os.File
	owned  *clipboardInput
	once   sync.Once
	err    error
}

func (w *clipboardOutput) initialize() error {
	w.once.Do(func() {
		owned, err := newClipboardInput(w.source)
		w.err = err
		if err == nil {
			w.file, w.owned = owned.file, owned
		}
	})
	return w.err
}
func (w *clipboardOutput) Close() error {
	w.once.Do(func() { w.err = os.ErrClosed })
	if w.owned == nil {
		return nil
	}
	return w.owned.Close()
}
func (w *clipboardOutput) Write(p []byte) (int, error) {
	if err := w.initialize(); err != nil {
		return 0, err
	}
	return w.file.Write(p)
}
func (w *clipboardOutput) WriteContext(ctx context.Context, p []byte) (int, error) {
	if ctx.Err() != nil {
		return 0, ctx.Err()
	}
	if err := w.initialize(); err != nil {
		return 0, err
	}
	stop := context.AfterFunc(ctx, func() { w.Close() })
	defer stop()
	n, err := w.file.Write(p)
	if ctx.Err() != nil {
		w.Close()
		return n, ctx.Err()
	}
	return n, err
}
