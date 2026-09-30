//go:build n1clipboarddiagnostic && !n1candidate

package supervisor

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"github.com/weshofmann/boxwarden/internal/clipboarddiag"
	"github.com/weshofmann/boxwarden/internal/clipboardx"
	"net"
	"testing"
	"time"
)

func TestDiagnosticControlInvalidExpiryBranchBeforeOwner(t *testing.T) {
	b := minimalRequest(t).Binding
	op := clipboarddiag.Operation{Version: 1, OperationID: "00000000-0000-4000-8000-000000000001", Direction: "read", Domain: "n1qualification", SessionID: "00000000-0000-4000-8000-000000000002", BackendKind: "tart", BackendObject: "synthetic", Generation: "00000000-0000-4000-8000-000000000003", ExpiresAt: time.Now().UTC().Add(time.Second)}
	r, err := clipboarddiag.NewRecorder(op, "supervisor", time.Now)
	if err != nil {
		t.Fatal(err)
	}
	ctx, err := clipboarddiag.WithContext(context.Background(), op, r)
	if err != nil {
		t.Fatal(err)
	}
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	owner := &clipboardTestOwner{binding: b}
	handleClipboardControl(ctx, server, b, owner, controlRequest{}, time.Now())
	events := r.Fragment().Records
	if owner.calls != 0 || len(events) != 1 || events[0].Stage != "control_expiry_missing" {
		t.Fatal("actual expiry branch unrecorded")
	}
}
func diagnosticControlBinding() Binding {
	return Binding{"n1qualification", "00000000-0000-4000-8000-000000000002", "tart", "synthetic", "00000000-0000-4000-8000-000000000003"}
}
func diagnosticControlOperation() clipboarddiag.Operation {
	b := diagnosticControlBinding()
	return clipboarddiag.Operation{Version: 1, OperationID: "00000000-0000-4000-8000-000000000001", Direction: "write", Domain: b.Domain, SessionID: b.SessionID, BackendKind: b.BackendKind, BackendObject: b.BackendObject, Generation: b.Generation, ExpiresAt: time.Now().UTC().Add(time.Second)}
}
func TestDiagnosticPrivateControlStrictFieldsExactGenerationAndOneUse(t *testing.T) {
	b := diagnosticControlBinding()
	op := diagnosticControlOperation()
	scope := clipboardDiagnosticScope(context.Background(), b)
	owner := &clipboardTestOwner{binding: b, ready: true, outcome: clipboardx.Committed}
	invoke := func(operation clipboarddiag.Operation, extra bool) bool {
		server, client := net.Pipe()
		done := make(chan struct{})
		request := diagnosticInvokeRequest{1, "n1_clipboard_invoke", b, operation}
		raw, _ := json.Marshal(request)
		if extra {
			raw = append(raw[:len(raw)-1], []byte(`,"extra":0}`)...)
		}
		go func() { handleControl(scope, server, b, owner, func() error { return nil }); close(done) }()
		client.SetDeadline(time.Now().Add(time.Second))
		writeFrame(client, raw)
		header, err := readBounded(client)
		if err != nil {
			client.Close()
			<-done
			return false
		}
		var ready clipboardControlReply
		if decodeExact(header, &ready) != nil {
			t.Fatal("malformed ready")
		}
		ctx, cancel := context.WithDeadline(context.Background(), op.ExpiresAt)
		defer cancel()
		transfer := &clipboardConnection{conn: client, binding: b, direction: clipboardx.ToGuest, ctx: ctx, cancel: cancel}
		outcome, err := transfer.Write(ctx, []byte("synthetic"))
		transfer.Close()
		<-done
		return err == nil && outcome == clipboardx.Committed
	}
	foreign := op
	foreign.Generation = "00000000-0000-4000-8000-000000000004"
	if invoke(foreign, false) || invoke(op, true) || owner.calls != 0 {
		t.Fatal("foreign or extended diagnostic control admitted")
	}
	if !invoke(op, false) || owner.calls != 1 {
		t.Fatal("exact invocation missing")
	}
	if invoke(op, false) || owner.calls != 1 {
		t.Fatal("one-use invocation replayed")
	}
	r, _ := clipboarddiag.NewRecorder(op, "cli", time.Now)
	cli := r.Fragment()
	collect := func() (clipboarddiag.CollectionReceipt, bool) {
		server, client := net.Pipe()
		done := make(chan struct{})
		raw, _ := json.Marshal(diagnosticCollectRequest{1, "n1_clipboard_collect", b, op, cli})
		go func() { handleControl(scope, server, b, owner, func() error { return nil }); close(done) }()
		client.SetDeadline(time.Now().Add(time.Second))
		writeFrame(client, raw)
		data, err := readClipboardDiagnosticFrame(client)
		client.Close()
		<-done
		var got clipboarddiag.CollectionReceipt
		if err != nil {
			return got, false
		}
		return got, clipboarddiag.StrictDecode(data, &got, clipboarddiag.MaxCollectionBytes) == nil && got.Validate() == nil
	}
	receipt, ok := collect()
	if !ok || receipt.Complete || owner.calls != 1 {
		t.Fatal("collection failed/invented guest completeness/replayed transfer")
	}
	if _, ok = collect(); ok {
		t.Fatal("one-use collection replayed")
	}
}
func TestDiagnosticCollectionFramingBoundAndTrailing(t *testing.T) {
	for _, raw := range [][]byte{make([]byte, 32768), []byte{1}} {
		var buffer bytes.Buffer
		err := writeClipboardDiagnosticFrame(&buffer, raw)
		if len(raw) == 32768 {
			if err == nil {
				t.Fatal("oversize admitted")
			}
			continue
		}
		buffer.WriteByte(1)
		if _, err := readClipboardDiagnosticFrame(&buffer); err == nil {
			t.Fatal("trailing bytes admitted")
		}
	}
}

type diagnosticBranchOwner struct {
	*clipboardTestOwner
	snapshots     int
	failAt        int
	kind          string
	afterDispatch func()
	onSnapshot    func()
}

func (o *diagnosticBranchOwner) Snapshot(ctx context.Context) Snapshot {
	o.snapshots++
	if o.onSnapshot != nil {
		o.onSnapshot()
	}
	s := o.clipboardTestOwner.Snapshot(ctx)
	if o.snapshots == o.failAt {
		if o.kind == "binding" {
			s.Binding.Generation = "00000000-0000-4000-8000-000000000009"
		} else {
			s.ProbeOK = false
		}
	}
	return s
}
func (o *diagnosticBranchOwner) WriteClipboard(ctx context.Context, text []byte) (clipboardx.Outcome, error) {
	result, err := o.clipboardTestOwner.WriteClipboard(ctx, text)
	if o.afterDispatch != nil {
		o.afterDispatch()
	}
	return result, err
}
func TestDiagnosticControlActualShortCircuitsAndFinalFrames(t *testing.T) {
	for _, kind := range []string{"expiry-elapsed", "expiry-bound", "binding", "ready", "ready-frame", "length", "source", "terminator", "redispatch-binding", "redispatch-ready", "post-binding", "post-ready", "outcome", "final-frame", "read-validation", "ok"} {
		t.Run(kind, func(t *testing.T) {
			op := diagnosticControlOperation()
			r, _ := clipboarddiag.NewRecorder(op, "supervisor", time.Now)
			ctx, _ := clipboarddiag.WithContext(context.Background(), op, r)
			server, client := net.Pipe()
			done := make(chan struct{})
			owner := &diagnosticBranchOwner{clipboardTestOwner: &clipboardTestOwner{binding: diagnosticControlBinding(), ready: true, outcome: clipboardx.Committed}}
			req := controlRequest{Action: "clipboard_write", ExpiresAt: op.ExpiresAt}
			stage := "control_final_ok_frame"
			expectedCalls := 0
			switch kind {
			case "expiry-elapsed":
				req.ExpiresAt = time.Now().Add(-time.Second)
				stage = "control_expiry_elapsed"
			case "expiry-bound":
				req.ExpiresAt = time.Now().Add(31 * time.Second)
				stage = "control_expiry_bound"
			case "binding", "ready":
				owner.failAt = 1
				owner.kind = kind
				stage = "control_" + kind
			case "ready-frame":
				stage = "control_ready_frame"
				owner.onSnapshot = func() { client.Close() }
			case "length":
				stage = "control_length"
			case "source":
				stage = "control_source"
			case "terminator":
				stage = "control_terminator"
			case "redispatch-binding", "redispatch-ready":
				owner.failAt = 2
				owner.kind = kind[len("redispatch-"):]
				stage = "control_" + kind
				stage = "control_redispatch_" + owner.kind
			case "post-binding", "post-ready":
				owner.failAt = 3
				owner.kind = kind[len("post-"):]
				stage = "control_post_" + owner.kind
				expectedCalls = 1
			case "outcome":
				owner.outcome = "synthetic-invalid"
				stage = "control_outcome"
				expectedCalls = 1
			case "final-frame":
				owner.afterDispatch = func() { client.Close() }
				stage = "control_final_ok_frame"
				expectedCalls = 1
			case "read-validation":
				req.Action = "clipboard_read"
				owner.data = []byte{0xff}
				stage = "control_read_validation"
				expectedCalls = 1
			case "ok":
				expectedCalls = 1
			}
			go func() {
				defer close(done)
				defer server.Close()
				handleClipboardControl(ctx, server, diagnosticControlBinding(), owner, req, time.Now())
			}()
			client.SetDeadline(time.Now().Add(time.Second))
			if kind != "expiry-elapsed" && kind != "expiry-bound" {
				raw, err := readBounded(client)
				if err == nil {
					var ready clipboardControlReply
					if decodeExact(raw, &ready) != nil {
						t.Fatal("frame")
					}
					if ready.Status == "ready" {
						if req.Action == "clipboard_write" {
							length := uint32(len("synthetic"))
							if kind == "length" {
								length = clipboardx.MaxTextBytes + 1
							}
							binary.Write(client, binary.BigEndian, length)
							if kind != "length" {
								text := []byte("synthetic")
								if kind == "source" {
									text = []byte{0xff} // declared source remains longer, actual EOF proves truncation.
									client.Write(text)
									client.Close()
								} else {
									client.Write(text)
								}
							}
						}
						if kind != "length" && kind != "source" {
							terminator := byte(0)
							if kind == "terminator" {
								terminator = 1
							}
							client.Write([]byte{terminator})
							_, _ = readBounded(client)
						}
					}
				}
			}
			client.Close()
			<-done
			if owner.calls != expectedCalls {
				t.Fatalf("actual dispatch count %d expected %d", owner.calls, expectedCalls)
			}
			found := false
			for _, record := range r.Fragment().Records {
				if record.Stage == stage {
					found = true
				}
				if kind == "final-frame" && record.Stage == "control_final_error_frame" {
					found = true
				}
			}
			if !found {
				t.Fatal("actual branch absent", stage, r.Fragment().Records)
			}
		})
	}
}
