//go:build darwin || linux

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"syscall"
	"testing"
	"time"
)

func TestOwnedClipboardInputCancellationAndOwnership(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	input, err := newClipboardInput(r)
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err = input.ReadContext(ctx, make([]byte, 1))
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > time.Second {
		t.Fatalf("blocking read %v", err)
	}
	if _, err := r.Stat(); err != nil {
		t.Fatal("closed caller fd")
	}
}

func TestOwnedClipboardOutputCancellationPreservesCallerDescriptor(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	output := &clipboardOutput{source: w}
	defer output.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()
	started := time.Now()
	n, err := output.WriteContext(ctx, make([]byte, 1<<20))
	if !errors.Is(err, context.DeadlineExceeded) || n >= 1<<20 || time.Since(started) > time.Second {
		t.Fatalf("blocked write n=%d err=%v", n, err)
	}
	if _, err := w.Stat(); err != nil {
		t.Fatal("closed caller descriptor")
	}
}

func clipboardFlagsForTest(t *testing.T, file *os.File) int {
	t.Helper()
	raw, err := file.SyscallConn()
	if err != nil {
		t.Fatal(err)
	}
	var flags uintptr
	var errno syscall.Errno
	err = raw.Control(func(fd uintptr) { flags, _, errno = syscall.Syscall(syscall.SYS_FCNTL, fd, syscall.F_GETFL, 0) })
	if err != nil || errno != 0 {
		t.Fatalf("get descriptor flags: %v %v", err, errno)
	}
	return int(flags)
}
func clipboardNonblockForTest(t *testing.T, file *os.File, value bool) {
	t.Helper()
	raw, err := file.SyscallConn()
	if err != nil {
		t.Fatal(err)
	}
	var setErr error
	if err := raw.Control(func(fd uintptr) { setErr = syscall.SetNonblock(int(fd), value) }); err != nil || setErr != nil {
		t.Fatalf("set descriptor flags: %v %v", err, setErr)
	}
}
func TestClipboardOwnedDescriptorsRestoreOriginalNonblock(t *testing.T) {
	for _, nonblock := range []bool{false, true} {
		for _, mode := range []string{"input", "output"} {
			t.Run(fmt.Sprintf("%s/nonblock=%v", mode, nonblock), func(t *testing.T) {
				r, w, err := os.Pipe()
				if err != nil {
					t.Fatal(err)
				}
				defer r.Close()
				defer w.Close()
				source := r
				if mode == "output" {
					source = w
				}
				clipboardNonblockForTest(t, source, nonblock)
				before := clipboardFlagsForTest(t, source)
				var close func() error
				if mode == "input" {
					input, err := newClipboardInput(source)
					if err != nil {
						t.Fatal(err)
					}
					close = input.Close
				} else {
					output := &clipboardOutput{source: source}
					if _, err := output.Write([]byte{}); err != nil {
						t.Fatal(err)
					}
					close = output.Close
				}
				if err := close(); err != nil {
					t.Fatal(err)
				}
				if err := close(); err != nil {
					t.Fatal(err)
				}
				after := clipboardFlagsForTest(t, source)
				if after&syscall.O_NONBLOCK != before&syscall.O_NONBLOCK {
					t.Fatalf("nonblock changed before=%x after=%x", before, after)
				}
				if _, err := source.Stat(); err != nil {
					t.Fatal("caller descriptor closed")
				}
			})
		}
	}
}
func TestClipboardCancellationRestoresOriginalNonblock(t *testing.T) {
	for _, mode := range []string{"input", "output"} {
		t.Run(mode, func(t *testing.T) {
			r, w, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			defer r.Close()
			defer w.Close()
			source := r
			if mode == "output" {
				source = w
			}
			clipboardNonblockForTest(t, source, false)
			before := clipboardFlagsForTest(t, source)
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
			defer cancel()
			if mode == "input" {
				input, err := newClipboardInput(source)
				if err != nil {
					t.Fatal(err)
				}
				defer input.Close()
				_, err = input.ReadContext(ctx, make([]byte, 1))
				if !errors.Is(err, context.DeadlineExceeded) {
					t.Fatal(err)
				}
			} else {
				output := &clipboardOutput{source: source}
				defer output.Close()
				_, err := output.WriteContext(ctx, make([]byte, 1<<20))
				if !errors.Is(err, context.DeadlineExceeded) {
					t.Fatal(err)
				}
			}
			if after := clipboardFlagsForTest(t, source); after&syscall.O_NONBLOCK != before&syscall.O_NONBLOCK {
				t.Fatalf("cancel flags before=%x after=%x", before, after)
			}
		})
	}
}

func TestClipboardOutputErrorRestoresCallerFlagsOnClose(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	clipboardNonblockForTest(t, w, false)
	before := clipboardFlagsForTest(t, w)
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	output := &clipboardOutput{source: w}
	if _, err := output.WriteContext(t.Context(), []byte("synthetic")); !errors.Is(err, syscall.EPIPE) {
		t.Fatalf("write error=%v", err)
	}
	if err := output.Close(); err != nil {
		t.Fatal(err)
	}
	if after := clipboardFlagsForTest(t, w); after&syscall.O_NONBLOCK != before&syscall.O_NONBLOCK {
		t.Fatalf("error cleanup flags before=%x after=%x", before, after)
	}
}
func TestClipboardConcurrentCloseRetainsCallerDescriptor(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	clipboardNonblockForTest(t, r, false)
	input, err := newClipboardInput(r)
	if err != nil {
		t.Fatal(err)
	}
	var wait sync.WaitGroup
	for count := 0; count < 16; count++ {
		wait.Go(func() {
			if err := input.Close(); err != nil {
				t.Error(err)
			}
		})
	}
	wait.Wait()
	if clipboardFlagsForTest(t, r)&syscall.O_NONBLOCK != 0 {
		t.Fatal("concurrent cleanup left changed flags")
	}
	if _, err := r.Stat(); err != nil {
		t.Fatal("concurrent cleanup closed caller descriptor")
	}
}

func TestClipboardRestoresFlagsOnlyAfterOwnedIOQuiesces(t *testing.T) {
	for _, mode := range []string{"input", "output"} {
		t.Run(mode, func(t *testing.T) {
			r, w, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			defer r.Close()
			defer w.Close()
			source := r
			if mode == "output" {
				source = w
			}
			clipboardNonblockForTest(t, source, false)
			owned, err := newClipboardInput(source)
			if err != nil {
				t.Fatal(err)
			}
			defer owned.Close()
			entered := make(chan struct{})
			release := make(chan struct{})
			owned.owned.closeIO = func() error {
				close(entered)
				<-release
				if clipboardFlagsForTest(t, source)&syscall.O_NONBLOCK == 0 {
					t.Error("blocking restored while owned IO can still retry")
				}
				err := owned.file.Close()
				if clipboardFlagsForTest(t, source)&syscall.O_NONBLOCK == 0 {
					t.Error("flags restored before IO close completed")
				}
				return err
			}
			ctx, cancel := context.WithCancel(t.Context())
			done := make(chan error, 1)
			if mode == "input" {
				go func() { _, err := owned.ReadContext(ctx, make([]byte, 1)); done <- err }()
			} else {
				output := &clipboardOutput{file: owned.file, owned: owned}
				output.once.Do(func() {})
				go func() { _, err := output.WriteContext(ctx, make([]byte, 1<<20)); done <- err }()
			}
			closing := make(chan error, 1)
			go func() { closing <- owned.Close() }()
			select {
			case <-entered:
				cancel()
			case <-time.After(time.Second):
				close(release)
				t.Fatal("cleanup did not begin")
			}
			close(release)
			if err := <-closing; err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-done:
				if !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
			case <-time.After(time.Second):
				t.Fatal("IO did not cancel")
			}
			if clipboardFlagsForTest(t, source)&syscall.O_NONBLOCK != 0 {
				t.Fatal("original flags not restored after quiescence")
			}
		})
	}
}
