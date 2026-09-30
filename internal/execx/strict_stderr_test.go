package execx

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"testing"
	"time"
)

type diagnosticReadCloseFault struct {
	*os.File
	kind   string
	closes int
}

func (f *diagnosticReadCloseFault) Close() error {
	f.closes++
	e := f.File.Close()
	if f.kind == "close-after-effect" {
		return io.ErrClosedPipe
	}
	return e
}
func (f *diagnosticReadCloseFault) Read(p []byte) (int, error) {
	n, e := f.File.Read(p)
	if f.kind == "read-error" && n > 0 {
		return n, io.ErrUnexpectedEOF
	}
	return n, e
}
func TestDiagnosticStrictStderrActualEOFReadCloseAndCancellation(t *testing.T) {
	for _, kind := range []string{"ok", "close-after-effect", "read-error", "no-eof", "cancel", "overflow"} {
		t.Run(kind, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 300*time.Millisecond)
			defer cancel()
			r, w, e := os.Pipe()
			if e != nil {
				t.Fatal(e)
			}
			deadline, _ := ctx.Deadline()
			r.SetReadDeadline(deadline)
			fault := &diagnosticReadCloseFault{File: r, kind: kind}
			buffer := newBoundedBuffer(512)
			raw := []byte("fixed-metadata")
			if kind == "overflow" {
				raw = make([]byte, 513)
			}
			w.Write(raw)
			if kind != "no-eof" && kind != "cancel" {
				w.Close()
			}
			done := make(chan bool, 1)
			go func() { done <- drainStrictStderr(ctx, fault, buffer) }()
			if kind == "cancel" {
				cancel()
			}
			var complete bool
			select {
			case complete = <-done:
			case <-time.After(time.Second):
				t.Fatal("owned drain leaked")
			}
			w.Close()
			if complete != (kind == "ok") || fault.closes != 1 || len(buffer.contents) > 512 || cap(buffer.contents) > 512 {
				t.Fatal("EOF/actual close/retention contract", complete, fault.closes)
			}
			if _, e = r.Stat(); e == nil {
				t.Fatal("actual reader not closed")
			}
		})
	}
}
func TestDiagnosticStrictStderrParentCloseLossPreservesWaitAndStdout(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestOSRunnerDiagnosticStreamChild$", "--", "fixed")
	cmd.Env = []string{"LANG=C", "GORACE=atexit_sleep_ms=0"}
	stdout := newBoundedBuffer(512)
	stderr := newBoundedBuffer(512)
	cmd.Stdout = stdout
	closes := 0
	e, complete := runStrictStderr(ctx, cmd, stderr, func(f *os.File) error {
		closes++
		actual := f.Close()
		if actual != nil {
			t.Fatal(actual)
		}
		return io.ErrClosedPipe
	})
	if e != nil || complete || closes != 1 || stdout.String() != "abcdef" || stderr.String() != "uvwxyz" {
		t.Fatal("metadata close changed original Wait/outcome", e, complete)
	}
}
func TestDiagnosticStrictStderrNoDeadlineDoesNotStart(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	e, complete := runStrictStderr(context.Background(), cmd, newBoundedBuffer(512), func(f *os.File) error { return f.Close() })
	if e == nil || complete || cmd.Process != nil {
		t.Fatal("deadline fallback/dispatch")
	}
}
func TestDiagnosticStrictStderrTransportChild(t *testing.T) {
	if len(os.Args) != 4 || os.Args[2] != "--" {
		return
	}
	fmt.Fprint(os.Stdout, "original-ack")
	fmt.Fprint(os.Stderr, "metadata")
	if os.Args[3] == "cancel" {
		time.Sleep(2 * time.Second)
	}
	os.Exit(7)
}
func TestDiagnosticStrictStderrExitAndCancelPreserveOriginalError(t *testing.T) {
	for _, kind := range []string{"exit", "cancel"} {
		t.Run(kind, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 300*time.Millisecond)
			defer cancel()
			result, e := (OSRunner{StrictStderr: true, MaxStdoutBytes: 512, MaxStderrBytes: 512}).Run(ctx, Command{Path: os.Args[0], Args: []string{"-test.run=^TestDiagnosticStrictStderrTransportChild$", "--", kind}, Env: []string{"LANG=C", "GORACE=atexit_sleep_ms=0"}})
			var exit *exec.ExitError
			if e == nil || !errors.As(e, &exit) || result.StderrComplete || result.Stdout != "original-ack" {
				t.Fatal("original exit/error lost", e)
			}
			if kind == "exit" && exit.ExitCode() != 7 {
				t.Fatal("exit code changed")
			}
		})
	}
}
