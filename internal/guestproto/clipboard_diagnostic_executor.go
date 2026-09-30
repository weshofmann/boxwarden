//go:build n1clipboarddiagnostic

package guestproto

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/weshofmann/boxwarden/internal/clipboarddiag"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"time"
)

const ClipboardDiagnosticHelperPath = "/usr/local/libexec/boxwarden-n1-clipboard-diagnostic"

// DiagnosticClipboardExecutor is installed only by the distinct fixed helper.
// Its operation is copied by value; ordinary Bootstrapper layout is untouched.
type DiagnosticClipboardExecutor struct {
	operation   clipboarddiag.Operation
	directory   string
	seal        func(string, string) error
	publication *diagnosticInvokePublication
}

func newDiagnosticClipboardExecutor(o clipboarddiag.Operation, dir string) DiagnosticClipboardExecutor {
	return DiagnosticClipboardExecutor{operation: o, directory: dir, seal: os.Link}
}
func (e DiagnosticClipboardExecutor) Run(ctx context.Context, direction string, payload []byte) ([]byte, error) {
	ctx = clipboarddiag.WithSource(ctx, "helper")
	if direction != e.operation.Direction || e.operation.Validate(true) != nil || ValidateClipboardText(payload) != nil || (direction == "read" && len(payload) != 0) {
		return nil, clipboarddiag.ErrMetadata
	}
	deadline, ok := ctx.Deadline()
	if !ok || !deadline.Equal(e.operation.ExpiresAt) || ctx.Err() != nil {
		return nil, clipboarddiag.ErrMetadata
	}
	for _, dir := range []string{"/usr", "/usr/local", "/usr/local/libexec"} {
		if safeDirectory(dir, 0755) != nil {
			return nil, clipboarddiag.ErrMetadata
		}
	}
	if _, err := readSafeFile(ClipboardHelperPath, 0755); err != nil {
		return nil, clipboarddiag.ErrMetadata
	}
	command := diagnosticClipboardCommand(ctx, direction, deadline, payload)
	raw, proof, err := runDiagnosticAdapter(ctx, command, e.operation)
	// Proof loss is ancillary metadata loss. It never substitutes for canonical
	// adapter execution error/success or changes the acknowledgement bytes.
	_, recorder, _ := clipboarddiag.Get(ctx)
	if proof == nil {
		if recorder != nil {
			recorder.Invalidate()
		}
		clipboarddiag.Record(ctx, "helper_proof", "incomplete")
	} else if publishDiagnosticClosure(e.directory, e.operation, *proof, e.seal) != nil {
		if recorder != nil {
			recorder.Invalidate()
		}
		clipboarddiag.Record(ctx, "helper_publish", "incomplete")
	} else {
		if e.publication != nil {
			e.publication.closureRaw, _ = clipboarddiag.EncodeClosure(*proof)
			e.publication.closureReturned = true
		}
		clipboarddiag.Record(ctx, "helper_publish", "ok")
	}
	return raw, err
}

// The production factory has no caller-selected executable, environment or fd.
func diagnosticClipboardCommand(ctx context.Context, direction string, deadline time.Time, payload []byte) *exec.Cmd {
	command := exec.CommandContext(ctx, "/usr/bin/python3", ClipboardHelperPath, direction, "--deadline-unix-ns", strconv.FormatInt(deadline.UnixNano(), 10))
	command.Env = []string{"HOME=/root", "USER=root", "LOGNAME=root", "PATH=/usr/bin:/bin", "LANG=C.UTF-8"}
	command.Dir = "/"
	command.Stdin = bytes.NewReader(payload)
	command.Stderr = nil
	command.WaitDelay = 2 * time.Second
	return command
}

// runDiagnosticAdapter is also exercised with a synthetic test executable. The
// production call above supplies the sole fixed adapter argv and environment.
func runDiagnosticAdapter(ctx context.Context, command *exec.Cmd, operation clipboarddiag.Operation) ([]byte, *clipboarddiag.Closure, error) {
	header, err := clipboarddiag.EncodeOperation(operation)
	if err != nil {
		return nil, nil, clipboarddiag.ErrMetadata
	}
	metaRead, metaWrite, err := os.Pipe()
	if err != nil {
		return nil, nil, clipboarddiag.ErrMetadata
	}
	defer metaRead.Close()
	n, writeErr := metaWrite.Write(header)
	closeErr := metaWrite.Close()
	if writeErr != nil || n != len(header) || closeErr != nil {
		return nil, nil, clipboarddiag.ErrMetadata
	}
	proofRead, proofWrite, err := diagnosticProofPipe()
	if err != nil {
		return nil, nil, clipboarddiag.ErrMetadata
	}
	command.ExtraFiles = []*os.File{metaRead, proofWrite}
	var output clipboardOutputBuffer
	command.Stdout = &output
	command.Stderr = nil
	clipboarddiag.Record(ctx, "helper_pipe", "ok")
	if !clipboarddiag.Available(ctx) {
		proofRead.Close()
		proofWrite.Close()
		return nil, nil, clipboarddiag.ErrMetadata
	}
	if err = command.Start(); err != nil {
		proofRead.Close()
		proofWrite.Close()
		return nil, nil, err
	}
	clipboarddiag.Record(ctx, "helper_start", "ok")
	// No writer copy is retained by the root parent after child inheritance.
	writerCloseErr := proofWrite.Close()
	metaCloseErr := metaRead.Close()
	runErr := command.Wait()
	proof, proofErr := readDiagnosticProof(proofRead, operation)
	if writerCloseErr != nil || metaCloseErr != nil || proofErr != nil {
		proof = nil
		_, r, _ := clipboarddiag.Get(ctx)
		if r != nil {
			r.Invalidate()
		}

	}
	if output.overflow {
		return nil, proof, errors.New("clipboard adapter output exceeds bound")
	}
	return output.Bytes(), proof, runErr
}

// NewFile preserves an already nonblocking descriptor through Fd and exec.
// os.Pipe marks its files for Fd to restore blocking mode, which would silently
// violate the child-observed fixed fd4 contract during ExtraFiles inheritance.
func diagnosticProofPipe() (*os.File, *os.File, error) {
	var descriptors [2]int
	// Match os.Pipe's fork exclusion: no concurrent exec may inherit the
	// descriptors in the interval before both close-on-exec flags are installed.
	syscall.ForkLock.RLock()
	pipeErr := syscall.Pipe(descriptors[:])
	if pipeErr == nil {
		syscall.CloseOnExec(descriptors[0])
		syscall.CloseOnExec(descriptors[1])
	}
	syscall.ForkLock.RUnlock()
	if pipeErr != nil {
		return nil, nil, clipboarddiag.ErrMetadata
	}
	for _, descriptor := range descriptors {
		if syscall.SetNonblock(descriptor, true) != nil {
			syscall.Close(descriptors[0])
			syscall.Close(descriptors[1])
			return nil, nil, clipboarddiag.ErrMetadata
		}
	}
	return os.NewFile(uintptr(descriptors[0]), "diagnostic-proof-read"), os.NewFile(uintptr(descriptors[1]), "diagnostic-proof-write"), nil
}
func readDiagnosticProof(reader io.ReadCloser, o clipboarddiag.Operation) (proof *clipboarddiag.Closure, err error) {
	start := time.Now()
	if f, ok := reader.(*os.File); ok {
		if f.SetReadDeadline(time.Now().Add(250*time.Millisecond)) != nil {
			f.Close()
			return nil, clipboarddiag.ErrMetadata
		}
	}
	raw, readErr := io.ReadAll(io.LimitReader(reader, 513))
	// Adjudicate the actual reader close even if framing/read already failed.
	closeErr := reader.Close()
	if end := time.Now(); readErr != nil || closeErr != nil || len(raw) > 512 || end.Before(start) || !end.Before(start.Add(250*time.Millisecond)) {
		return nil, clipboarddiag.ErrMetadata
	}
	value, decodeErr := clipboarddiag.DecodeClosure(raw, o)
	if decodeErr != nil {
		return nil, clipboarddiag.ErrMetadata
	}
	return &value, nil
}
func publishDiagnosticClosure(directory string, o clipboarddiag.Operation, c clipboarddiag.Closure, seal func(string, string) error) error {
	if c.Validate(o) != nil || seal == nil {
		return clipboarddiag.ErrMetadata
	}
	raw, err := clipboarddiag.EncodeClosure(c)
	if err != nil {
		return clipboarddiag.ErrMetadata
	}
	return publishDiagnosticFile(directory, o.Direction+".closure", raw, seal)
}
func publishDiagnosticFile(directory, name string, raw []byte, seal func(string, string) error) error {
	pending := filepath.Join(directory, name+".pending")
	file, err := os.OpenFile(pending, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0600)
	if err != nil {
		return clipboarddiag.ErrMetadata
	}
	return finalizeDiagnosticPending(directory, name, file, raw, seal)
}

// Strict current-generation namespace metadata omits direction/op/expiry. It is
// derived from the existing serial publication, never from caller authority.
type diagnosticGeneration struct {
	Version int `json:"version"`
	Association
	Generation string `json:"generation"`
}

func (b *Bootstrapper) admitDiagnosticNamespace(o clipboarddiag.Operation, create bool) (string, error) {
	dir, _, err := b.admitDiagnosticNamespacePublication(o, create)
	return dir, err
}
func (b *Bootstrapper) admitDiagnosticNamespacePublication(o clipboarddiag.Operation, create bool) (string, bool, error) {
	if os.Geteuid() != 0 || o.Validate(false) != nil {
		return "", false, clipboarddiag.ErrMetadata
	}
	dir, err := b.clipboardRuntimeDirectory(false)
	if err != nil {
		return "", false, clipboarddiag.ErrMetadata
	}
	run, _ := b.path("run")
	if diagnosticRootMetadata(run, 0755, true, 0) != nil || diagnosticRootMetadata(dir, 0700, true, 0) != nil {
		return "", false, clipboarddiag.ErrMetadata
	}
	current, err := readClipboardGeneration(filepath.Join(dir, "clipboard-generation.json"))
	if err != nil || current.Association != (Association{Domain: o.Domain, SessionID: o.SessionID, BackendKind: o.BackendKind, BackendObject: o.BackendObject}) || current.Generation != o.Generation {
		return "", false, clipboarddiag.ErrMetadata
	}
	if b.validateBootstrapAncestors() != nil {
		return "", false, clipboarddiag.ErrMetadata
	}
	target := filepath.Join(dir, "n1-clipboard-diagnostic")
	created := false
	if create {
		err = os.Mkdir(target, 0700)
		if err == nil {
			created = true
			if syncDiagnosticDirectory(dir) != nil {
				return "", false, clipboarddiag.ErrMetadata
			}
		} else if !errors.Is(err, os.ErrExist) {
			return "", false, clipboarddiag.ErrMetadata
		}
	}
	if diagnosticRootMetadata(target, 0700, true, 0) != nil {
		return "", false, clipboarddiag.ErrMetadata
	}
	expected := diagnosticGeneration{1, current.Association, current.Generation}
	raw, _ := json.Marshal(expected)
	raw = append(raw, '\n')
	generationPath := filepath.Join(target, "generation.binding")
	if created {
		if publishDiagnosticFile(target, "generation.binding", raw, os.Link) != nil {
			return "", false, clipboarddiag.ErrMetadata
		}
	} else {
		got, e := readDiagnosticFile(generationPath, 4096)
		if e != nil || !bytes.Equal(got, raw) {
			return "", false, clipboarddiag.ErrMetadata
		}
	}
	// A reboot's /run absence is admitted only by the invoke path's fresh mkdir.
	// Existing absent/foreign generation metadata is never adopted or repaired.
	return target, created, nil
}
func diagnosticRootMetadata(path string, mode os.FileMode, directory bool, limit int64) error {
	info, err := os.Lstat(path)
	if err != nil {
		return clipboarddiag.ErrMetadata
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || info.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 || st.Uid != 0 || st.Gid != 0 || info.Mode().Perm() != mode || info.Mode()&os.ModeSymlink != 0 || info.IsDir() != directory || (!directory && (!info.Mode().IsRegular() || st.Nlink != 1 || info.Size() > limit)) || diagnosticNoACL(path, directory) != nil {
		return clipboarddiag.ErrMetadata
	}
	return nil
}
func readDiagnosticFile(path string, limit int) ([]byte, error) {
	if !diagnosticPublicationCommitted(path) || diagnosticRootMetadata(path, 0600, false, int64(limit)) != nil {
		return nil, clipboarddiag.ErrMetadata
	}
	before, _ := os.Lstat(path)
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, clipboarddiag.ErrMetadata
	}
	opened, e := f.Stat()
	if e != nil || !os.SameFile(before, opened) {
		f.Close()
		return nil, clipboarddiag.ErrMetadata
	}
	raw, readErr := io.ReadAll(io.LimitReader(f, int64(limit)+1))
	after, e := f.Stat()
	closeErr := f.Close()
	nameAfter, nameErr := os.Lstat(path)
	if readErr != nil || closeErr != nil || e != nil || nameErr != nil || len(raw) > limit || !os.SameFile(before, after) || !os.SameFile(before, nameAfter) || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) || (!diagnosticPublicationCommitted(path) || diagnosticRootMetadata(path, 0600, false, int64(limit)) != nil) {
		return nil, clipboarddiag.ErrMetadata
	}
	return raw, nil
}

type diagnosticPendingFile interface {
	Write([]byte) (int, error)
	Sync() error
	Close() error
}

func finishDiagnosticPending(file diagnosticPendingFile, raw []byte, seal func() error) error {
	n, writeErr := file.Write(raw)
	syncErr := file.Sync()
	closeErr := file.Close()
	if writeErr != nil || n != len(raw) || syncErr != nil || closeErr != nil {
		return clipboarddiag.ErrMetadata
	}
	// Actual close is adjudicated before any final namespace publication.
	if seal == nil || seal() != nil {
		return clipboarddiag.ErrMetadata
	}
	return nil
}
