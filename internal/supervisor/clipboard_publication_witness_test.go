//go:build n1clipboarddiagnostic && !n1candidate

package supervisor

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/weshofmann/boxwarden/internal/clipboarddiag"
	"github.com/weshofmann/boxwarden/internal/clipboardx"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

type diagnosticPublicationOwner struct {
	*clipboardTestOwner
	guest     clipboarddiag.GuestCollection
	collects  int
	witness   *clipboarddiag.PublicationWitness
	duplicate bool
}

func (o *diagnosticPublicationOwner) CollectClipboardDiagnostic(context.Context, clipboarddiag.Operation) (clipboarddiag.GuestCollection, error) {
	o.collects++
	return o.guest, nil
}
func publicationGuestFixture(t *testing.T, op clipboarddiag.Operation) clipboarddiag.GuestCollection {
	t.Helper()
	raw, e := os.ReadFile("../clipboarddiag/testdata/task2-r1-receipts.json")
	if e != nil {
		t.Fatal(e)
	}
	var overlays []clipboarddiag.OverlayReceipt
	if json.Unmarshal(raw, &overlays) != nil {
		t.Fatal("fixture")
	}
	overlay := overlays[1]
	if op.Direction == "read" {
		overlay = overlays[0]
	}
	overlay.Binding = op
	overlay.HeaderDigest = clipboarddiag.HeaderDigest(op)
	overlay.Closure.HeaderDigest = overlay.HeaderDigest
	for i := range overlay.Records {
		overlay.Records[i].HeaderDigest = overlay.HeaderDigest
	}
	r, _ := clipboarddiag.NewRecorder(op, "guest", time.Now)
	ctx, _ := clipboarddiag.WithContext(t.Context(), op, r)
	clipboarddiag.Record(clipboarddiag.WithSource(ctx, "helper"), "helper_namespace", "ok")
	clipboarddiag.Record(ctx, "guest_complete", "ok")
	clipboarddiag.Record(clipboarddiag.WithSource(ctx, "helper"), "helper_response", "ok")
	f := r.Fragment()
	g := clipboarddiag.GuestCollection{Version: 1, Binding: op, Complete: true, Bootstrap: &f, Overlay: &overlay}
	if g.Validate(op) != nil {
		t.Fatal("valid complete fixture")
	}
	return g
}
func TestDiagnosticCollectionRequiresAcknowledgedPublication(t *testing.T) {
	op := diagnosticControlOperation()
	b := diagnosticControlBinding()
	parent := clipboardDiagnosticScope(t.Context(), b)
	scope := parent.Value(diagnosticScopeKey{}).(*diagnosticScope)
	r, _ := clipboarddiag.NewRecorder(op, "supervisor", time.Now)
	ctx, _ := clipboarddiag.WithContext(t.Context(), op, r)
	clipboarddiag.Record(ctx, "control_final_ok_frame", "ok")
	scope.directions[op.Direction] = &diagnosticEntry{operation: op, recorder: r, finished: true}
	cli, _ := clipboarddiag.NewRecorder(op, "cli", time.Now)
	cctx, _ := clipboarddiag.WithContext(t.Context(), op, cli)
	clipboarddiag.Record(cctx, "cli_binding", "ok")
	clipboarddiag.Record(cctx, "cli_outcome", "ok")
	owner := &diagnosticPublicationOwner{clipboardTestOwner: &clipboardTestOwner{binding: b, ready: true}, guest: publicationGuestFixture(t, op)}
	server, client := net.Pipe()
	defer client.Close()
	done := make(chan struct{})
	raw, _ := json.Marshal(diagnosticCollectRequest{1, "n1_clipboard_collect", b, op, cli.Fragment()})
	go func() {
		handleClipboardDiagnosticControl(parent, server, b, owner, raw, time.Now())
		server.Close()
		close(done)
	}()
	client.SetDeadline(time.Now().Add(time.Second))
	data, e := readClipboardDiagnosticFrame(client)
	<-done
	var receipt clipboarddiag.CollectionReceipt
	if e != nil || clipboarddiag.StrictDecode(data, &receipt, 32768) != nil {
		t.Fatal("collection frame", e)
	}
	if receipt.Complete {
		t.Fatal("complete-looking artifacts admitted without successful publication-return witness")
	}
	if owner.collects != 1 {
		t.Fatal("collection was replayed")
	}
}

func (o *diagnosticPublicationOwner) ReadClipboard(ctx context.Context) ([]byte, error) {
	text, e := o.clipboardTestOwner.ReadClipboard(ctx)
	if o.witness != nil {
		clipboarddiag.ObservePublication(ctx, *o.witness)
	}
	return text, e
}
func (o *diagnosticPublicationOwner) WriteClipboard(ctx context.Context, text []byte) (clipboardx.Outcome, error) {
	out, e := o.clipboardTestOwner.WriteClipboard(ctx, text)
	if o.witness != nil {
		clipboarddiag.ObservePublication(ctx, *o.witness)
		if o.duplicate {
			clipboarddiag.ObservePublication(ctx, *o.witness)
		}
	}
	return out, e
}
func publicationInvokeWitness(op clipboarddiag.Operation, g clipboarddiag.GuestCollection, created bool) clipboarddiag.PublicationWitness {
	bootstrap, _ := json.Marshal(g.Bootstrap)
	bootstrap = append(bootstrap, '\n')
	closure, _ := clipboarddiag.EncodeClosure(*g.Overlay.Closure)
	b := clipboarddiag.MetadataDigest(bootstrap)
	c := clipboarddiag.MetadataDigest(closure)
	return clipboarddiag.PublicationWitness{V: 1, Phase: "invoke", Header: clipboarddiag.HeaderDigest(op), Generation: clipboarddiag.GenerationDigest(op), Created: created, Bootstrap: &b, Closure: &c}
}
func invokePublicationControl(t *testing.T, parent context.Context, b Binding, op clipboarddiag.Operation, owner *diagnosticPublicationOwner) clipboardx.Outcome {
	t.Helper()
	server, client := net.Pipe()
	done := make(chan struct{})
	raw, _ := json.Marshal(diagnosticInvokeRequest{1, "n1_clipboard_invoke", b, op})
	go func() {
		handleClipboardDiagnosticControl(parent, server, b, owner, raw, time.Now())
		server.Close()
		close(done)
	}()
	client.SetDeadline(time.Now().Add(time.Second))
	if _, e := readBounded(client); e != nil {
		client.Close()
		<-done
		t.Fatal("ready", e)
	}
	ctx, cancel := context.WithDeadline(t.Context(), op.ExpiresAt)
	transfer := &clipboardConnection{conn: client, binding: b, direction: clipboardx.ToGuest, ctx: ctx, cancel: cancel}
	var out clipboardx.Outcome
	if op.Direction == "read" {
		transfer.direction = clipboardx.FromGuest
		text, err := transfer.Read(ctx)
		if err != nil || !bytes.Equal(text, []byte("fixed-benign")) {
			t.Fatal("prior read transfer", err)
		}
		out = clipboardx.Unchanged
	} else {
		out, _ = transfer.Write(ctx, []byte("fixed-benign"))
	}
	transfer.Close()
	<-done
	return out
}
func collectPublicationControl(t *testing.T, parent context.Context, b Binding, op clipboarddiag.Operation, owner *diagnosticPublicationOwner) clipboarddiag.CollectionReceipt {
	t.Helper()
	r, _ := clipboarddiag.NewRecorder(op, "cli", time.Now)
	ctx, _ := clipboarddiag.WithContext(t.Context(), op, r)
	clipboarddiag.Record(ctx, "cli_binding", "ok")
	clipboarddiag.Record(ctx, "cli_outcome", "unknown")
	raw, _ := json.Marshal(diagnosticCollectRequest{1, "n1_clipboard_collect", b, op, r.Fragment()})
	server, client := net.Pipe()
	done := make(chan struct{})
	go func() {
		handleClipboardDiagnosticControl(parent, server, b, owner, raw, time.Now())
		server.Close()
		close(done)
	}()
	client.SetDeadline(time.Now().Add(time.Second))
	data, e := readClipboardDiagnosticFrame(client)
	client.Close()
	<-done
	var receipt clipboarddiag.CollectionReceipt
	if e != nil || clipboarddiag.StrictDecode(data, &receipt, 32768) != nil || receipt.Validate() != nil {
		t.Fatal("collection", e)
	}
	return receipt
}
func TestDiagnosticPublicationHostGenerationHashesAndUnknownCompleteness(t *testing.T) {
	for _, kind := range []string{"valid-unknown", "missing", "reused-missing-creation", "reused-known-creation", "wrong-header", "wrong-generation", "wrong-bootstrap", "wrong-closure", "duplicate", "after-effect-bootstrap", "after-effect-closure", "after-effect-generation"} {
		t.Run(kind, func(t *testing.T) {
			op := diagnosticControlOperation()
			b := diagnosticControlBinding()
			parent := clipboardDiagnosticScope(t.Context(), b)
			guest := publicationGuestFixture(t, op)
			w := publicationInvokeWitness(op, guest, true)
			owner := &diagnosticPublicationOwner{clipboardTestOwner: &clipboardTestOwner{binding: b, ready: true, outcome: clipboardx.Unknown}, guest: guest, witness: &w}
			switch kind {
			case "missing":
				owner.witness = nil
			case "reused-missing-creation", "reused-known-creation":
				w.Created = false
				prior := op
				prior.Direction = "read"
				prior.OperationID = "00000000-0000-4000-8000-000000000009"
				priorGuest := publicationGuestFixture(t, prior)
				priorWitness := publicationInvokeWitness(prior, priorGuest, true)
				priorOwner := &diagnosticPublicationOwner{clipboardTestOwner: &clipboardTestOwner{binding: b, ready: true, data: []byte("fixed-benign")}, guest: priorGuest, witness: &priorWitness}
				if kind == "reused-missing-creation" {
					priorOwner.witness = nil
				}
				invokePublicationControl(t, parent, b, prior, priorOwner)
				if priorOwner.calls != 1 {
					t.Fatal("prior creation witness was not from one actual transfer")
				}
			case "wrong-header":
				other := op
				other.ExpiresAt = other.ExpiresAt.Add(time.Nanosecond)
				w.Header = clipboarddiag.HeaderDigest(other)
			case "wrong-generation":
				w.Generation = strings.Repeat("a", 64)
			case "wrong-bootstrap":
				h := strings.Repeat("a", 64)
				w.Bootstrap = &h
			case "wrong-closure":
				h := strings.Repeat("a", 64)
				w.Closure = &h
			case "duplicate":
				owner.duplicate = true
			}
			if strings.HasPrefix(kind, "after-effect-") {
				dir := t.TempDir()
				name := "write.bootstrap"
				raw, _ := json.Marshal(guest.Bootstrap)
				raw = append(raw, '\n')
				if kind == "after-effect-closure" {
					name = "write.closure"
					raw, _ = clipboarddiag.EncodeClosure(*guest.Overlay.Closure)
				}
				if kind == "after-effect-generation" {
					name = "generation.binding"
					raw = clipboarddiag.GenerationMetadata(op)
				}
				pending := filepath.Join(dir, name+".pending")
				final := filepath.Join(dir, name)
				if os.WriteFile(pending, raw, 0600) != nil || os.Link(pending, final) != nil {
					t.Fatal("effect fixture")
				}
				unlink := func(path string) error {
					if e := syscall.Unlink(path); e != nil {
						return e
					}
					return io.ErrClosedPipe
				}
				publicationErr := unlink(pending)
				info, e := os.Lstat(final)
				if publicationErr == nil || e != nil || info.Sys().(*syscall.Stat_t).Nlink != 1 {
					t.Fatal("actual after-effect state")
				}
				got, e := os.ReadFile(final)
				if e != nil || !bytes.Equal(raw, got) {
					t.Fatal("complete-looking published bytes lost")
				}
				owner.witness = nil
			}
			out := invokePublicationControl(t, parent, b, op, owner)
			receipt := collectPublicationControl(t, parent, b, op, owner)
			want := kind == "valid-unknown" || kind == "reused-known-creation"
			if out != clipboardx.Unknown || receipt.Complete != want || owner.calls != 1 || owner.collects != 1 {
				t.Fatal("publication gate/outcome/one transfer", out, receipt.Complete, owner.calls, owner.collects)
			}
		})
	}
}
