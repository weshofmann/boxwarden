package fixed

import (
	"errors"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/contract"
	"io"
	"os"
	"os/exec"
	"time"
)

// WaitCloseout passes only the phase3 witness already validated from U's actual
// retained R return. F has no privileged or lifecycle dispatch capability.
func WaitCloseout(v Inputs, w contract.Witness, deadline time.Time) (ChildResult, error) {
	if !v.Lock.Valid() || !v.Handoff.Valid() || v.Handoff.Window.LockSHA != v.LockSHA || !w.Valid() || w.Phase != 3 || w.LockSHA != v.LockSHA || w.WindowID != v.Handoff.Window.ID || w.HandoffSHA != v.HandoffSHA || CheckArtifact(5, v.Lock) != nil {
		return ChildResult{}, ErrRefused
	}
	c, sha, e := ReadCompletion()
	if e != nil || sha != w.CompletionSHA || c.Window != v.Handoff.Window || c.HandoffSHA != v.HandoffSHA || c.ArchiveSHA != v.Handoff.ArchiveSHA {
		return ChildResult{}, ErrRefused
	}
	raw, e := contract.EncodeWitness(w)
	if e != nil {
		return ChildResult{}, ErrRefused
	}
	return waitCompletionChild(contract.ArtifactPath(5), nil, Environment(false), raw, deadline, func(f *os.File) error { return f.Close() }, func(w io.Writer, b []byte) (int, error) { return w.Write(b) })
}

// The injected command seam is private and used only by harmless child tests.
func waitCompletionChild(path string, args, env []string, raw []byte, deadline time.Time, closeFile func(*os.File) error, write func(io.Writer, []byte) (int, error)) (ChildResult, error) {
	result := ChildResult{Exit: -1}
	witness, e := contract.ParseWitness(raw)
	if e != nil || witness.Phase != 3 || closeFile == nil || write == nil || !time.Now().Before(deadline) {
		return result, ErrRefused
	}
	r, w, e := os.Pipe()
	if e != nil {
		return result, ErrRefused
	}
	n, we := write(w, raw)
	ce := closeFile(w)
	if we != nil || n != len(raw) || ce != nil {
		return result, errors.Join(ErrRefused, we, ce, closeFile(r))
	}
	out, ow, e := os.Pipe()
	if e != nil {
		return result, errors.Join(ErrRefused, closeFile(r))
	}
	er, ew, e := os.Pipe()
	if e != nil {
		return result, errors.Join(ErrRefused, closeFile(r), closeFile(out), closeFile(ow))
	}
	cmd := exec.Command(path, args...)
	cmd.Env = env
	cmd.ExtraFiles = []*os.File{r}
	cmd.Stdout = ow
	cmd.Stderr = ew
	if !time.Now().Before(deadline) {
		return result, errors.Join(ErrRefused, closeFile(r), closeFile(out), closeFile(ow), closeFile(er), closeFile(ew))
	}
	if e = cmd.Start(); e != nil {
		return result, errors.Join(ErrRefused, closeFile(r), closeFile(out), closeFile(ow), closeFile(er), closeFile(ew))
	}
	writers := errors.Join(closeFile(r), closeFile(ow), closeFile(ew))
	drain := func(f *os.File, ch chan<- drainResult) {
		b, e := io.ReadAll(io.LimitReader(f, contract.MaxWitnessBytes+1))
		if len(b) > contract.MaxWitnessBytes {
			b = nil
			e = ErrRefused
		}
		ch <- drainResult{b, errors.Join(e, closeFile(f))}
	}
	stdout, stderr := make(chan drainResult, 1), make(chan drainResult, 1)
	go drain(out, stdout)
	go drain(er, stderr)
	timer := time.AfterFunc(time.Until(deadline), func() { closeFile(out); closeFile(er) })
	waitErr := cmd.Wait()
	result.Exit = cmd.ProcessState.ExitCode()
	a, b := <-stdout, <-stderr
	timer.Stop()
	result.Raw = a.raw
	result.Closed = writers == nil && a.err == nil && b.err == nil
	if waitErr != nil || result.Exit != 0 || !result.Closed || len(a.raw) != 0 || len(b.raw) != 0 || !time.Now().Before(deadline) {
		return result, ErrRefused
	}
	return result, nil
}
