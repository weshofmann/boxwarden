//go:build n1clipboarddiagnostic

package guestproto

import (
	"bytes"
	"context"
	"github.com/weshofmann/boxwarden/internal/clipboarddiag"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func transportOperation() clipboarddiag.Operation {
	return clipboarddiag.Operation{Version: 1, OperationID: "00000000-0000-4000-8000-000000000001", Direction: "write", Domain: "n1qualification", SessionID: "00000000-0000-4000-8000-000000000002", BackendKind: "tart", BackendObject: "synthetic", Generation: "00000000-0000-4000-8000-000000000003", ExpiresAt: time.Now().UTC().Add(5 * time.Second)}
}
func TestDiagnosticChildSyntheticFixedDescriptorContract(t *testing.T) {
	if len(os.Args) < 4 || os.Args[len(os.Args)-2] != "--n1-transport" {
		return
	}
	mode := os.Args[len(os.Args)-1]
	fd3 := os.NewFile(3, "metadata")
	raw, e := io.ReadAll(fd3)
	if e != nil {
		os.Exit(31)
	}
	fd3.Close()
	op, e := clipboarddiag.DecodeOperation(raw, true)
	if e != nil {
		os.Exit(32)
	}
	stderr := os.NewFile(2, "stderr")
	stderrInfo, stderrErr := stderr.Stat()
	if stderrErr != nil || stderrInfo.Mode()&os.ModeCharDevice == 0 {
		os.Exit(37)
	}
	// This child sees actual descriptors/EOF. It never invokes Python/GTK/clipboard.
	if len(os.Args) != 5 || os.Args[2] != "--" || os.Args[3] != "--n1-transport" {
		os.Exit(35)
	}
	flags, _, errno := syscall.Syscall(syscall.SYS_FCNTL, 4, syscall.F_GETFL, 0)
	if errno != 0 || flags&syscall.O_NONBLOCK == 0 || flags&syscall.O_ACCMODE != syscall.O_WRONLY {
		os.Exit(36)
	}
	fd4 := os.NewFile(4, "closure")
	info, e := fd4.Stat()
	if e != nil || info.Mode()&os.ModeNamedPipe == 0 {
		os.Exit(33)
	}
	if len(os.Environ()) != 5 || os.Getenv("HOME") != "/root" || os.Getenv("PATH") != "/usr/bin:/bin" {
		os.Exit(34)
	}
	if mode != "missing" {
		proof := clipboarddiag.Closure{Version: 1, HeaderDigest: clipboarddiag.HeaderDigest(op), Direction: op.Direction, ElapsedMS: 1}
		data, _ := clipboarddiag.EncodeClosure(proof)
		if mode == "trailing" {
			data = append(data, 'x')
		}
		if mode == "oversize" {
			data = bytes.Repeat([]byte("x"), 513)
		}
		fd4.Write(data)
	}
	fd4.Close()
	io.WriteString(os.Stdout, "{\"version\":1,\"status\":\"ok\",\"length\":2}\n")
	os.Exit(0)
}
func TestDiagnosticAdapterActualPipesEOFAndProofLossPreserveFrame(t *testing.T) {
	for _, mode := range []string{"ok", "missing", "trailing", "oversize"} {
		t.Run(mode, func(t *testing.T) {
			op := transportOperation()
			cmd := exec.CommandContext(context.Background(), os.Args[0], "-test.run=TestDiagnosticChildSyntheticFixedDescriptorContract", "--", "--n1-transport", mode)
			cmd.Env = []string{"HOME=/root", "USER=root", "LOGNAME=root", "PATH=/usr/bin:/bin", "LANG=C.UTF-8"}
			raw, proof, err := runDiagnosticAdapter(context.Background(), cmd, op)
			if err != nil || string(raw) != "{\"version\":1,\"status\":\"ok\",\"length\":2}\n" {
				t.Fatalf("canonical synthetic frame/outcome changed: %v", err)
			}
			if (proof != nil) != (mode == "ok") {
				t.Fatal("invalid/missing transport proof admitted")
			}
		})
	}
}
func TestDiagnosticProofRequiresActualReaderCloseAndEOF(t *testing.T) {
	op := transportOperation()
	raw, _ := clipboarddiag.EncodeClosure(clipboarddiag.Closure{Version: 1, HeaderDigest: clipboarddiag.HeaderDigest(op), Direction: op.Direction, ElapsedMS: 1})
	if _, err := readDiagnosticProof(&closeFailure{Reader: bytes.NewReader(raw)}, op); err == nil {
		t.Fatal("failed actual reader close passed")
	}
	r, w, _ := os.Pipe()
	w.Write(raw)
	start := time.Now()
	if _, err := readDiagnosticProof(r, op); err == nil || time.Since(start) > time.Second {
		t.Fatal("retained writer passed or unbounded")
	}
	w.Close()
}

type closeFailure struct{ io.Reader }

func (*closeFailure) Close() error { return io.ErrClosedPipe }
func TestDiagnosticClosureExclusivePublishAndNoOverwrite(t *testing.T) {
	dir := t.TempDir()
	op := transportOperation()
	proof := clipboarddiag.Closure{Version: 1, HeaderDigest: clipboarddiag.HeaderDigest(op), Direction: op.Direction, ElapsedMS: 1}
	seal := os.Link
	if err := publishDiagnosticClosure(dir, op, proof, seal); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "write.closure"))
	if err != nil || !strings.HasSuffix(string(got), "\n") {
		t.Fatal("proof missing")
	}
	if err := publishDiagnosticClosure(dir, op, proof, seal); err == nil {
		t.Fatal("published proof overwritten")
	}
	after, _ := os.ReadFile(filepath.Join(dir, "write.closure"))
	if !bytes.Equal(got, after) {
		t.Fatal("existing proof changed")
	}
}
func TestDiagnosticProofForkOwnerMustNotRetainInheritedWriter(t *testing.T) {
	// Real fork/EOF with synthetic bytes only. Child closes stdout/stderr as a
	// detached owner would; retaining fd4 alone must invalidate parent proof.
	for _, retain := range []string{"no", "yes"} {
		t.Run(retain, func(t *testing.T) {
			script := `import os,sys,json,hashlib,time
raw=os.read(3,4096);os.close(3)
pid=os.fork()
if pid==0:
 if sys.argv[1]=='no': os.close(4)
 os.close(1);os.close(2);time.sleep(.6);os._exit(0)
proof={'version':1,'header_digest':hashlib.sha256(raw).hexdigest(),'direction':'write','elapsed_ms':1}
os.write(4,json.dumps(proof,sort_keys=True,separators=(',',':')).encode()+b'\n');os.close(4)
os.write(1,b'{"version":1,"status":"ok","length":2}\n');os._exit(0)
`
			file := filepath.Join(t.TempDir(), "synthetic.py")
			if os.WriteFile(file, []byte(script), 0600) != nil {
				t.Fatal("fixture write")
			}
			op := transportOperation()
			cmd := exec.CommandContext(t.Context(), "/usr/bin/python3", file, retain)
			cmd.Env = []string{"HOME=/root", "USER=root", "LOGNAME=root", "PATH=/usr/bin:/bin", "LANG=C.UTF-8"}
			raw, proof, err := runDiagnosticAdapter(t.Context(), cmd, op)
			if err != nil || len(raw) == 0 {
				t.Fatal("synthetic outcome changed", err)
			}
			if (proof != nil) != (retain == "no") {
				t.Fatal("retained writer or actual childclose wrongly adjudicated")
			}
		})
	}
}

type diagnosticPendingFailure struct {
	writeErr, syncErr, closeErr error
	short                       bool
	closed                      bool
}

func (f *diagnosticPendingFailure) Write(raw []byte) (int, error) {
	if f.short {
		return len(raw) - 1, nil
	}
	return len(raw), f.writeErr
}
func (f *diagnosticPendingFailure) Sync() error  { return f.syncErr }
func (f *diagnosticPendingFailure) Close() error { f.closed = true; return f.closeErr }
func TestDiagnosticPendingFailureNeverPublishes(t *testing.T) {
	for _, kind := range []string{"write", "short", "sync", "close", "publish"} {
		t.Run(kind, func(t *testing.T) {
			f := &diagnosticPendingFailure{}
			switch kind {
			case "write":
				f.writeErr = io.ErrShortWrite
			case "short":
				f.short = true
			case "sync":
				f.syncErr = io.ErrClosedPipe
			case "close":
				f.closeErr = io.ErrClosedPipe
			}
			calls := 0
			err := finishDiagnosticPending(f, []byte("synthetic"), func() error {
				calls++
				if !f.closed {
					t.Fatal("seal preceded actual close")
				}
				return io.ErrClosedPipe
			})
			if err == nil || !f.closed || (calls != 0 && kind != "publish") {
				t.Fatal("failed metadata published")
			}
		})
	}
}

func TestDiagnosticCollectionIntakeHonorsMetadataDeadline(t *testing.T) {
	reader, writer := io.Pipe()
	defer reader.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- NewBootstrapper("/", nil).collectDiagnosticClipboard(ctx, reader, io.Discard) }()
	select {
	case <-done:
		writer.Close()
	case <-time.After(100 * time.Millisecond):
		writer.Close()
		<-done
		t.Fatal("stalled metadata intake exceeded context budget")
	}
}

func TestDiagnosticProductionFactoryPreservesOriginalArgvDeadlineAndClosedEnvironment(t *testing.T) {
	op := transportOperation()
	command := diagnosticClipboardCommand(t.Context(), op.Direction, op.ExpiresAt, []byte("synthetic"))
	want := []string{"/usr/bin/python3", ClipboardHelperPath, "write", "--deadline-unix-ns", strconv.FormatInt(op.ExpiresAt.UnixNano(), 10)}
	if command.Path != "/usr/bin/python3" || !reflect.DeepEqual(command.Args, want) || command.Dir != "/" || !reflect.DeepEqual(command.Env, []string{"HOME=/root", "USER=root", "LOGNAME=root", "PATH=/usr/bin:/bin", "LANG=C.UTF-8"}) {
		t.Fatal("fixed factory contract changed")
	}
	raw, _ := io.ReadAll(command.Stdin)
	if string(raw) != "synthetic" {
		t.Fatal("ordinary private payload changed")
	}
}

func TestDiagnosticPublicationHardlinkRequiresFinalCommit(t *testing.T) {
	dir := t.TempDir()
	op := transportOperation()
	proof := clipboarddiag.Closure{Version: 1, HeaderDigest: clipboarddiag.HeaderDigest(op), Direction: op.Direction, ElapsedMS: 1}
	if err := publishDiagnosticClosure(dir, op, proof, os.Link); err != nil {
		t.Fatal(err)
	}
	final := filepath.Join(dir, "write.closure")
	if _, err := os.Lstat(final + ".pending"); !os.IsNotExist(err) {
		t.Fatal("publication reported success while provisional guard remained")
	}
	info, err := os.Lstat(final)
	if err != nil || info.Sys().(*syscall.Stat_t).Nlink != 1 {
		t.Fatal("collector-visible artifact not single-link committed")
	}
}
