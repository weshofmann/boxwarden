//go:build n1clipboarddiagnostic && !n1candidate

package sshx

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"time"
)

type n1ProcessResult struct {
	output []byte
	exit   int
}

// n1Execute retains only its actual child. Pipes are independently owned so
// actual Wait, EOF, checked read-close and stderr completion are distinct facts.
// Deadline closes read handles to release held-descendant EOF, never signals a
// reconstructed PID, process group or another consumer. Failure discards bytes.
func n1Execute(ctx context.Context, command Command, limit int, consume func(io.Reader) error) (n1ProcessResult, error) {
	if ctx.Err() != nil || limit < 1 || limit > 16384 || len(command.Stdin) > n1MaxStageBytes+65540 {
		return n1ProcessResult{}, ErrN1Guest
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	process := exec.CommandContext(ctx, command.Path, command.Args...)
	process.WaitDelay = 250 * time.Millisecond
	process.Env = []string{"LC_ALL=C", "LANG=C", "TZ=UTC"}
	process.Stdin = bytes.NewReader(command.Stdin)
	outR, outW, err := os.Pipe()
	if err != nil {
		return n1ProcessResult{}, ErrN1Guest
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		outR.Close()
		outW.Close()
		return n1ProcessResult{}, ErrN1Guest
	}
	process.Stdout = outW
	process.Stderr = errW
	if process.Start() != nil {
		outR.Close()
		outW.Close()
		errR.Close()
		errW.Close()
		return n1ProcessResult{}, ErrN1Guest
	}
	writeCloseErr := errors.Join(outW.Close(), errW.Close())
	if writeCloseErr != nil {
		cancel()
	}
	type readResult struct {
		bytes []byte
		err   error
	}
	stdout := make(chan readResult, 1)
	stderr := make(chan readResult, 1)
	wait := make(chan error, 1)
	go func() { wait <- process.Wait() }()
	go func() {
		var raw []byte
		var e error
		if consume == nil {
			raw, e = n1ReadBounded(outR, limit)
		} else {
			e = consume(outR)
		}
		e = errors.Join(e, outR.Close())
		if e != nil {
			cancel()
		}
		stdout <- readResult{raw, e}
	}()
	go func() {
		raw, e := n1ReadBounded(errR, 4096)
		e = errors.Join(e, errR.Close())
		if e != nil {
			cancel()
		}
		stderr <- readResult{raw, e}
	}()
	var out, diagnostic readResult
	var waitErr error
	outDone, errDone, waitDone := false, false, false
	// Inherited pipes cannot keep this retained child observation open forever.
	// Context closure releases actual readers; all owned goroutines are joined.
	for !outDone || !errDone || !waitDone {
		select {
		case out = <-stdout:
			outDone = true
			stdout = nil
		case diagnostic = <-stderr:
			errDone = true
			stderr = nil
		case waitErr = <-wait:
			waitDone = true
			wait = nil
		case <-ctx.Done():
			cancel()
			outR.Close()
			errR.Close()
			// Reads can now finish, including an inherited writer held by a descendant.
			if !outDone {
				out = <-stdout
				outDone = true
			}
			if !errDone {
				diagnostic = <-stderr
				errDone = true
			}
			if !waitDone {
				waitErr = <-wait
				waitDone = true
			}
		}
	}
	exit := 0
	if waitErr != nil {
		var ee *exec.ExitError
		if errors.As(waitErr, &ee) {
			exit = ee.ExitCode()
		} else {
			return n1ProcessResult{}, ErrN1Guest
		}
	}
	if writeCloseErr != nil || out.err != nil || diagnostic.err != nil || len(diagnostic.bytes) > 0 || ctx.Err() != nil {
		return n1ProcessResult{}, ErrN1Guest
	}
	return n1ProcessResult{out.bytes, exit}, nil
}

func n1ReadBounded(reader io.Reader, limit int) ([]byte, error) {
	// Allocate the fixed limit before reading. Never append an over-limit prefix.
	buffer := make([]byte, limit+1)
	total := 0
	for {
		n, err := reader.Read(buffer[total:])
		total += n
		if total > limit {
			return nil, ErrN1Guest
		}
		if err == io.EOF {
			return buffer[:total], nil
		}
		if err != nil {
			return nil, ErrN1Guest
		}
		if n == 0 {
			return nil, ErrN1Guest
		}
	}
}

func n1ReadRecords(reader io.Reader, record func([]byte) error) error {
	buffer := make([]byte, 8192)
	length, total, count := 0, 0, 0
	one := make([]byte, 1)
	for {
		n, err := reader.Read(one)
		if n > 0 {
			total++
			if total > 16384 || length >= len(buffer) {
				return ErrN1Guest
			}
			if one[0] == '\n' {
				count++
				if count > 2 || length == 0 || record(buffer[:length]) != nil {
					return ErrN1Guest
				}
				length = 0
			} else {
				buffer[length] = one[0]
				length++
			}
		}
		if err == io.EOF {
			if length != 0 || count != 2 {
				return ErrN1Guest
			}
			return nil
		}
		if err != nil || n == 0 {
			return ErrN1Guest
		}
	}
}

// Decode the one complete receipt before waiting for EOF. Its typed facts can
// survive a later retained-child/wait/drain failure; arbitrary bytes cannot.
func n1ReadSingleRecord(reader io.Reader, limit int, decode func([]byte) error) error {
	buffer := make([]byte, limit)
	length := 0
	one := make([]byte, 1)
	for {
		n, err := reader.Read(one)
		if n > 0 {
			if length >= limit {
				return ErrN1Guest
			}
			if one[0] == '\n' {
				if length == 0 || decode(buffer[:length]) != nil {
					return ErrN1Guest
				}
				n, e := reader.Read(one)
				if n != 0 || e != io.EOF {
					return ErrN1Guest
				}
				return nil
			}
			buffer[length] = one[0]
			length++
		}
		if err != nil || n == 0 {
			return ErrN1Guest
		}
	}
}
