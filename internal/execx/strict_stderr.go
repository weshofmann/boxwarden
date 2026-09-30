package execx

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"
)

// Exactly one actual read-close is shared by EOF and context cancellation.
// The owned writer is closed by the parent immediately after successful Start.
func drainStrictStderr(ctx context.Context, reader io.ReadCloser, buffer *boundedBuffer) bool {
	var once sync.Once
	var closeErr error
	closeReader := func() { once.Do(func() { closeErr = reader.Close() }) }
	cancelDone := make(chan struct{})
	stop := context.AfterFunc(ctx, func() { closeReader(); close(cancelDone) })
	var scratch [512]byte
	eof := false
	for {
		n, e := reader.Read(scratch[:])
		if n > 0 {
			buffer.Write(scratch[:n])
		}
		if e != nil {
			eof = e == io.EOF
			break
		}
		if n == 0 {
			break
		}
	}
	closeReader()
	if !stop() {
		<-cancelDone
	}
	deadline, ok := ctx.Deadline()
	return eof && closeErr == nil && ctx.Err() == nil && ok && time.Now().Before(deadline) && !buffer.Truncated()
}

// The private closer parameter supports actual-close-then-error fault controls;
// production always passes os.File.Close and cannot select a descriptor/path.
func runStrictStderr(ctx context.Context, process *exec.Cmd, stderr *boundedBuffer, closeWriter func(*os.File) error) (error, bool) {
	deadline, ok := ctx.Deadline()
	if !ok || !time.Now().Before(deadline) {
		return fmt.Errorf("strict stderr requires original deadline"), false
	}
	reader, writer, e := os.Pipe()
	if e != nil {
		return fmt.Errorf("strict stderr pipe unavailable"), false
	}
	if reader.SetReadDeadline(deadline) != nil {
		reader.Close()
		writer.Close()
		return fmt.Errorf("strict stderr deadline unavailable"), false
	}
	process.Stderr = writer
	if e = process.Start(); e != nil {
		writer.Close()
		reader.Close()
		return e, false
	}
	done := make(chan bool, 1)
	go func() { done <- drainStrictStderr(ctx, reader, stderr) }()
	writerErr := closeWriter(writer)
	waitErr := process.Wait()
	observed := <-done
	return waitErr, observed && writerErr == nil && waitErr == nil && ctx.Err() == nil && time.Now().Before(deadline) && !stderr.Truncated()
}
