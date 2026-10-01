package caller

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/weshofmann/boxwarden/internal/qualification/n1/contract"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/fixed"
)

// The C waiter is deliberately separate from fixed.WaitChild's exit-zero contract.
func waitCoordinator(path string, args, env []string, stdin io.Reader, deadline time.Time, closeFile func(*os.File) error) (fixed.ChildResult, error) {
	result := fixed.ChildResult{Exit: -1}
	if !time.Now().Before(deadline) {
		return result, fixed.ErrRefused
	}
	out, ow, e := os.Pipe()
	if e != nil {
		return result, fixed.ErrRefused
	}
	er, ew, e := os.Pipe()
	if e != nil {
		return result, errors.Join(fixed.ErrRefused, closeFile(out), closeFile(ow))
	}
	cmd := exec.Command(path, args...)
	cmd.Env = env
	cmd.Stdin = stdin
	cmd.Stdout = ow
	cmd.Stderr = ew
	if e = cmd.Start(); e != nil {
		return result, errors.Join(fixed.ErrRefused, closeFile(out), closeFile(ow), closeFile(er), closeFile(ew))
	}
	writerErr := errors.Join(closeFile(ow), closeFile(ew))
	type drained struct {
		raw []byte
		err error
	}
	oc, ec := make(chan drained, 1), make(chan drained, 1)
	var stop sync.Once
	abort := func() { stop.Do(func() { cmd.Process.Kill(); closeFile(out); closeFile(er) }) }
	drain := func(f *os.File, limit int, c chan<- drained) {
		raw, e := io.ReadAll(io.LimitReader(f, int64(limit)+1))
		if len(raw) > limit {
			raw = nil
			e = fixed.ErrRefused
			abort()
		}
		ce := closeFile(f)
		c <- drained{raw, errors.Join(e, ce)}
	}
	go drain(out, contract.MaxReceiptBytes, oc)
	go drain(er, 4096, ec)
	timer := time.AfterFunc(time.Until(deadline), abort)
	we := cmd.Wait()
	o, s := <-oc, <-ec
	timer.Stop()
	result.Exit = cmd.ProcessState.ExitCode()
	result.Closed = writerErr == nil && o.err == nil && s.err == nil
	// Never retain or expose raw diagnostic stdout/stderr. C's success output is empty.
	if result.Exit != contract.PlannedExit || !result.Closed || len(o.raw) != 0 || len(s.raw) != 0 || !time.Now().Before(deadline) {
		return result, fixed.ErrRefused
	}
	var exit *exec.ExitError
	if !errors.As(we, &exit) || exit.ExitCode() != contract.PlannedExit {
		return result, fixed.ErrRefused
	}
	return result, nil
}
