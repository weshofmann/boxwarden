package fixed

import (
	"errors"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/contract"
	"io"
	"os"
	"os/exec"
	"time"
)

type ChildResult struct {
	Raw    []byte
	Exit   int
	Closed bool
}
type drainResult struct {
	raw []byte
	err error
}

// Direct retained command ownership. No PID reconstruction, kill, retry or
// status proxy is used. Necessary Wait/reap may outlive dispatch expiry; it
// never permits another command after expiry.
func WaitChild(path string, args, env []string, stdin io.Reader, deadline time.Time) (ChildResult, error) {
	return waitChild(path, args, env, stdin, deadline, func(f *os.File) error { return f.Close() })
}
func waitChild(path string, args, env []string, stdin io.Reader, deadline time.Time, closeFile func(*os.File) error) (ChildResult, error) {
	var result ChildResult
	result.Exit = -1
	if !time.Now().Before(deadline) {
		return result, ErrRefused
	}
	out, ow, e := os.Pipe()
	if e != nil {
		return result, ErrRefused
	}
	er, ew, e := os.Pipe()
	if e != nil {
		return result, errors.Join(ErrRefused, closeFile(out), closeFile(ow))
	}
	cmd := exec.Command(path, args...)
	cmd.Env = env
	cmd.Stdin = stdin
	cmd.Stdout = ow
	cmd.Stderr = ew
	if e = cmd.Start(); e != nil {
		return result, errors.Join(ErrRefused, closeFile(out), closeFile(ow), closeFile(er), closeFile(ew))
	}
	closeWriters := errors.Join(closeFile(ow), closeFile(ew))
	drain := func(f *os.File, cap int, ch chan<- drainResult) {
		raw, e := io.ReadAll(io.LimitReader(f, int64(cap)+1))
		if len(raw) > cap {
			e = ErrRefused
			raw = nil
		}
		ce := closeFile(f)
		ch <- drainResult{raw, errors.Join(e, ce)}
	}
	stdout := make(chan drainResult, 1)
	stderr := make(chan drainResult, 1)
	go drain(out, contract.MaxWitnessBytes, stdout)
	go drain(er, 4096, stderr)
	// Reader deadlines include EOF held by an unexpected descendant. Actual
	// process Wait remains mandatory even when drains/deadline refuse.
	timer := time.AfterFunc(time.Until(deadline), func() { closeFile(out); closeFile(er) })
	waitErr := cmd.Wait()
	result.Exit = cmd.ProcessState.ExitCode()
	o, s := <-stdout, <-stderr
	timer.Stop()
	result.Raw = o.raw
	result.Closed = closeWriters == nil && o.err == nil && s.err == nil
	if waitErr != nil || result.Exit != 0 || !result.Closed || len(s.raw) != 0 || !time.Now().Before(deadline) {
		return result, ErrRefused
	}
	return result, nil
}
func Environment(root bool) []string {
	user, home := "devel", "/Users/devel"
	if root {
		user, home = "root", "/var/root"
	}
	return []string{"HOME=" + home, "USER=" + user, "LOGNAME=" + user, "PATH=/usr/bin:/bin", "LC_ALL=C", "LANG=C"}
}
func ValidateChild(v Inputs, r ChildResult, phase uint8) (contract.Witness, error) {
	w, e := contract.ParseWitness(r.Raw)
	if e != nil || w.Phase != phase || w.LockSHA != v.LockSHA || w.WindowID != v.Handoff.Window.ID || w.HandoffSHA != v.HandoffSHA || w.Exit != r.Exit || r.Exit != 0 || !r.Closed {
		return contract.Witness{}, ErrRefused
	}
	c, sha, e := ReadCompletion()
	if e != nil || sha != w.CompletionSHA || c.Window != v.Handoff.Window || c.HandoffSHA != v.HandoffSHA || c.ArchiveSHA != v.Handoff.ArchiveSHA {
		return contract.Witness{}, ErrRefused
	}
	return w, nil
}
func WriteWitness(w contract.Witness) error {
	raw, e := contract.EncodeWitness(w)
	if e != nil {
		return ErrRefused
	}
	n, e := os.Stdout.Write(raw)
	if e != nil || n != len(raw) {
		return ErrRefused
	}
	return nil
}
