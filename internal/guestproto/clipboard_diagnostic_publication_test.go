//go:build n1clipboarddiagnostic

package guestproto

import (
	"bytes"
	"encoding/json"
	"github.com/weshofmann/boxwarden/internal/clipboarddiag"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

type diagnosticFaultDirectory struct {
	*os.File
	kind   string
	closed *bool
}

func (f *diagnosticFaultDirectory) Sync() error {
	err := f.File.Sync()
	if f.kind == "dir-sync" {
		return io.ErrClosedPipe
	}
	return err
}
func (f *diagnosticFaultDirectory) Close() error {
	err := f.File.Close()
	*f.closed = true
	if f.kind == "dir-close-after-effect" {
		return io.ErrClosedPipe
	}
	return err
}

type diagnosticFaultDataFile struct {
	*os.File
	kind string
}

func (f *diagnosticFaultDataFile) Write(raw []byte) (int, error) {
	if f.kind == "short" {
		return f.File.Write(raw[:len(raw)-1])
	}
	return f.File.Write(raw)
}
func (f *diagnosticFaultDataFile) Sync() error {
	err := f.File.Sync()
	if f.kind == "data-sync" {
		return io.ErrClosedPipe
	}
	return err
}
func (f *diagnosticFaultDataFile) Close() error {
	err := f.File.Close()
	if f.kind == "data-close-after-effect" {
		return io.ErrClosedPipe
	}
	return err
}

func TestDiagnosticPublicationFailuresAreVisibleToCollector(t *testing.T) {
	op := transportOperation()
	r, _ := clipboarddiag.NewRecorder(op, "guest", time.Now)
	ctx, _ := clipboarddiag.WithContext(t.Context(), op, r)
	clipboarddiag.Record(clipboarddiag.WithSource(ctx, "helper"), "helper_namespace", "ok")
	clipboarddiag.Record(clipboarddiag.WithSource(ctx, "helper"), "helper_response", "ok")
	bootstrap, _ := json.Marshal(r.Fragment())
	bootstrap = append(bootstrap, '\n')
	closure, _ := clipboarddiag.EncodeClosure(clipboarddiag.Closure{Version: 1, HeaderDigest: clipboarddiag.HeaderDigest(op), Direction: op.Direction, ElapsedMS: 1})
	for name, raw := range map[string][]byte{"write.bootstrap": bootstrap, "write.closure": closure, "generation.binding": []byte("fixed-synthetic-generation\n"), "write.collect": []byte(clipboarddiag.HeaderDigest(op) + "\n")} {
		for _, kind := range []string{"ok", "short", "data-sync", "data-close-after-effect", "seal-after-effect", "dir-sync", "dir-close-after-effect", "unlink", "foreign-inode"} {
			t.Run(name+"/"+kind, func(t *testing.T) {
				dir := t.TempDir()
				pending := filepath.Join(dir, name+".pending")
				final := filepath.Join(dir, name)
				file, err := os.OpenFile(pending, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
				if err != nil {
					t.Fatal(err)
				}
				closed := false
				unlinks := 0
				seal := func(from, to string) error {
					if _, e := file.Stat(); e == nil {
						t.Fatal("seal preceded actual data close")
					}
					if e := os.Link(from, to); e != nil {
						return e
					}
					if diagnosticPublicationCommitted(to) {
						t.Fatal("concurrent collection admitted provisional nlink2")
					}
					if kind == "seal-after-effect" {
						return io.ErrClosedPipe
					}
					if kind == "foreign-inode" {
						if e := os.Remove(from); e != nil {
							t.Fatal(e)
						}
						if e := os.WriteFile(from, []byte("foreign"), 0600); e != nil {
							t.Fatal(e)
						}
					}
					return nil
				}
				finalize := func(path string) error {
					return syncDiagnosticDirectoryWith(path, func(path string) (diagnosticDirectory, error) {
						f, e := os.Open(path)
						if e != nil {
							return nil, e
						}
						return &diagnosticFaultDirectory{f, kind, &closed}, nil
					})
				}
				unlink := func(path string) error {
					unlinks++
					if !closed || diagnosticPublicationCommitted(final) || path != pending {
						t.Fatal("commit preceded checked directory close or wrong resource")
					}
					if kind == "unlink" {
						return io.ErrClosedPipe
					}
					return syscall.Unlink(path)
				}
				err = finalizeDiagnosticPendingWith(dir, name, &diagnosticFaultDataFile{file, kind}, raw, seal, finalize, unlink)
				if (err == nil) != (kind == "ok") || diagnosticPublicationCommitted(final) != (kind == "ok") {
					t.Fatal("publication/collector disagreed with actual finalization", err)
				}
				if kind == "ok" {
					got, e := os.ReadFile(final)
					if e != nil || !bytes.Equal(got, raw) || unlinks != 1 {
						t.Fatal("committed bytes changed")
					}
				} else {
					if _, e := os.Lstat(pending); e != nil {
						t.Fatal("failed transaction lost guard", e)
					}
					if kind == "dir-close-after-effect" && !closed {
						t.Fatal("close fault did not occur after actual close")
					}
					if kind != "unlink" && unlinks != 0 {
						t.Fatal("failed transaction attempted commit")
					}
				}
			})
		}
	}
}
func TestDiagnosticPublicationNoOverwriteAndResidue(t *testing.T) {
	dir := t.TempDir()
	final := filepath.Join(dir, "read.closure")
	if os.WriteFile(final, []byte("existing"), 0600) != nil {
		t.Fatal("fixture")
	}
	if publishDiagnosticFile(dir, "read.closure", []byte("new"), os.Link) == nil {
		t.Fatal("overwrite accepted")
	}
	got, _ := os.ReadFile(final)
	if string(got) != "existing" || diagnosticPublicationCommitted(final) {
		t.Fatal("foreign state overwritten or residue admitted")
	}
	if publishDiagnosticFile(dir, "read.closure", []byte("retry"), os.Link) == nil {
		t.Fatal("pending reservation adopted/retried")
	}
}

func TestDiagnosticPublicationFailurePreservesActualGuestTransfer(t *testing.T) {
	b, request, fake, ctx, recorder := diagnosticGuestFixture(t)
	clipboarddiag.Record(clipboarddiag.WithSource(ctx, "helper"), "helper_namespace", "ok")
	response, text, err := b.Clipboard(ctx, request, []byte("ok"))
	if err != nil || response.Status != "ok" || fake.calls != 1 {
		t.Fatal("ordinary transfer fixture")
	}
	before, err := EncodeClipboardResponse(request, response, text)
	if err != nil {
		t.Fatal(err)
	}
	clipboarddiag.Record(clipboarddiag.WithSource(ctx, "helper"), "helper_response", "ok")
	raw, _ := json.Marshal(recorder.Fragment())
	raw = append(raw, '\n')
	dir := t.TempDir()
	file, err := os.OpenFile(filepath.Join(dir, "write.bootstrap.pending"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		t.Fatal(err)
	}
	closed := false
	err = finalizeDiagnosticPendingWith(dir, "write.bootstrap", file, raw, os.Link, func(path string) error {
		return syncDiagnosticDirectoryWith(path, func(path string) (diagnosticDirectory, error) {
			f, e := os.Open(path)
			return &diagnosticFaultDirectory{f, "dir-close-after-effect", &closed}, e
		})
	}, syscall.Unlink)
	if err == nil || !closed || diagnosticPublicationCommitted(filepath.Join(dir, "write.bootstrap")) {
		t.Fatal("late close failure admitted")
	}
	recorder.Invalidate()
	after, _ := EncodeClipboardResponse(request, response, text)
	if !bytes.Equal(before, after) || fake.calls != 1 || response.Status != "ok" || recorder.Fragment().Complete {
		t.Fatal("metadata failure changed original frame/outcome or replayed transfer")
	}
}
