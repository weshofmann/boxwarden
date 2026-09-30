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
)

func TestDiagnosticPublicationAfterEffectErrorHasNoReturnWitness(t *testing.T) {
	op := transportOperation()
	closure, _ := clipboarddiag.EncodeClosure(clipboarddiag.Closure{Version: 1, HeaderDigest: clipboarddiag.HeaderDigest(op), Direction: op.Direction, ElapsedMS: 1})
	for _, name := range []string{"write.bootstrap", "write.closure", "generation.binding", "write.collect"} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			pending := filepath.Join(dir, name+".pending")
			file, e := os.OpenFile(pending, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
			if e != nil {
				t.Fatal(e)
			}
			unlinks := 0
			raw := []byte("fixed-typed-metadata\n")
			e = finalizeDiagnosticPendingWith(dir, name, file, raw, os.Link, syncDiagnosticDirectory, func(p string) error {
				unlinks++
				if x := syscall.Unlink(p); x != nil {
					return x
				}
				return io.ErrClosedPipe
			})
			if e == nil || unlinks != 1 || !diagnosticPublicationCommitted(filepath.Join(dir, name)) {
				t.Fatal("after-effect fixture did not produce complete-looking namespace")
			}
			state := &diagnosticInvokePublication{operation: op, created: true, namespaceReturned: true, closureReturned: true, bootstrapReturned: true, bootstrapRaw: raw, closureRaw: closure}
			switch name {
			case "write.bootstrap":
				state.bootstrapReturned = e == nil
			case "write.closure":
				state.closureReturned = e == nil
			case "generation.binding":
				state.namespaceReturned = e == nil
			case "write.collect":
				state = nil
			}
			var output bytes.Buffer
			if name == "write.collect" {
				emitDiagnosticCollectionPublication(&output, op, e)
			} else {
				state.emit(&output)
			}
			if output.Len() != 0 {
				t.Fatal("error-after-effect became a witness")
			}
			before, _ := os.ReadFile(filepath.Join(dir, name))
			if retryErr := publishDiagnosticFile(dir, name, raw, os.Link); retryErr == nil {
				t.Fatal("consumed final overwritten")
			}
			after, _ := os.ReadFile(filepath.Join(dir, name))
			if !bytes.Equal(before, after) {
				t.Fatal("sealed bytes rewritten")
			}
			if unlinks != 1 {
				t.Fatal("unlink retry")
			}
		})
	}
}
func TestDiagnosticNamespaceParentActualCloseLossHasNoWitness(t *testing.T) {
	dir := t.TempDir()
	created := filepath.Join(dir, "n1-clipboard-diagnostic")
	if os.Mkdir(created, 0700) != nil {
		t.Fatal("mkdir")
	}
	closed := false
	e := syncDiagnosticDirectoryWith(dir, func(path string) (diagnosticDirectory, error) {
		f, e := os.Open(path)
		return &diagnosticFaultDirectory{f, "dir-close-after-effect", &closed}, e
	})
	state := &diagnosticInvokePublication{operation: transportOperation(), namespaceReturned: e == nil, closureReturned: true, bootstrapReturned: true}
	var output bytes.Buffer
	state.emit(&output)
	if e == nil || !closed || output.Len() != 0 {
		t.Fatal("namespace close after-effect fabricated acknowledgement")
	}
}

func TestDiagnosticGenerationWitnessUsesExistingExactBytes(t *testing.T) {
	op := transportOperation()
	raw, e := json.Marshal(diagnosticGeneration{1, Association{op.Domain, op.SessionID, op.BackendKind, op.BackendObject}, op.Generation})
	raw = append(raw, '\n')
	if e != nil || !bytes.Equal(raw, clipboarddiag.GenerationMetadata(op)) {
		t.Fatal("generation bytes changed")
	}
}

func TestDiagnosticPublicationUnlinkAfterEffectPreservesActualGuestFrame(t *testing.T) {
	b, request, fake, ctx, recorder := diagnosticGuestFixture(t)
	clipboarddiag.Record(clipboarddiag.WithSource(ctx, "helper"), "helper_namespace", "ok")
	response, text, e := b.Clipboard(ctx, request, []byte("ok"))
	if e != nil || response.Status != "ok" || fake.calls != 1 {
		t.Fatal("actual guest transfer fixture", e)
	}
	before, e := EncodeClipboardResponse(request, response, text)
	if e != nil {
		t.Fatal(e)
	}
	clipboarddiag.Record(clipboarddiag.WithSource(ctx, "helper"), "helper_response", "ok")
	raw, _ := json.Marshal(recorder.Fragment())
	raw = append(raw, '\n')
	dir := t.TempDir()
	file, e := os.OpenFile(filepath.Join(dir, "write.bootstrap.pending"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		t.Fatal(e)
	}
	unlinks := 0
	publicationErr := finalizeDiagnosticPendingWith(dir, "write.bootstrap", file, raw, os.Link, syncDiagnosticDirectory, func(path string) error {
		unlinks++
		if e := syscall.Unlink(path); e != nil {
			return e
		}
		return io.ErrClosedPipe
	})
	if publicationErr == nil || !diagnosticPublicationCommitted(filepath.Join(dir, "write.bootstrap")) {
		t.Fatal("after-effect error missing")
	}
	recorder.Invalidate()
	after, _ := EncodeClipboardResponse(request, response, text)
	op, _, _ := clipboarddiag.Get(ctx)
	state := &diagnosticInvokePublication{operation: op, namespaceReturned: true, closureReturned: true, bootstrapReturned: publicationErr == nil}
	var proof bytes.Buffer
	state.emit(&proof)
	if !bytes.Equal(before, after) || fake.calls != 1 || unlinks != 1 || response.Status != "ok" || proof.Len() != 0 {
		t.Fatal("original ACK/outcome/one-use changed")
	}
}
