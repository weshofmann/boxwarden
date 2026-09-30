//go:build n1clipboarddiagnostic

package guestproto

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/weshofmann/boxwarden/internal/clipboarddiag"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func diagnosticGuestFixture(t *testing.T) (*Bootstrapper, ClipboardRequest, *clipboardFake, context.Context, *clipboarddiag.Recorder) {
	t.Helper()
	b, req, fake := clipboardFixture(t)
	req.Domain = "n1qualification"
	serial := testRequest()
	serial.Domain = req.Domain
	// This fixture reboots no VM; it constructs the existing synthetic binding files.
	b, _ = testBootstrapper(t)
	if _, err := b.Serial(context.Background(), serial); err != nil {
		t.Fatal(err)
	}
	b.ClipboardExecutor = fake
	op := clipboarddiag.Operation{Version: 1, OperationID: "00000000-0000-4000-8000-000000000001", Domain: req.Domain, SessionID: req.SessionID, BackendKind: req.BackendKind, BackendObject: req.BackendObject, Generation: req.Generation, Direction: req.Direction, ExpiresAt: req.ExpiresAt}
	r, _ := clipboarddiag.NewRecorder(op, "guest", time.Now)
	ctx, _ := clipboarddiag.WithContext(context.Background(), op, r)
	return b, req, fake, ctx, r
}
func TestDiagnosticGuestActualBranchNoRetryAndOutcomePreservation(t *testing.T) {
	for _, kind := range []string{"explicit-error", "missing", "malformed", "execution", "length", "ok", "loss"} {
		t.Run(kind, func(t *testing.T) {
			b, req, fake, ctx, r := diagnosticGuestFixture(t)
			want := "unknown"
			stage := "guest_execution"
			fake.fn = func() ([]byte, error) {
				switch kind {
				case "explicit-error":
					return []byte("{\"version\":1,\"status\":\"error\",\"length\":0}\n"), errors.New("synthetic")
				case "missing":
					return nil, nil
				case "malformed":
					return []byte("{}\n"), nil
				case "execution":
					return []byte("{\"version\":1,\"status\":\"ok\",\"length\":2}\n"), errors.New("synthetic")
				case "length":
					return []byte("{\"version\":1,\"status\":\"ok\",\"length\":3}\n"), nil
				case "loss":
					r.Invalidate()
				}
				return []byte("{\"version\":1,\"status\":\"ok\",\"length\":2}\n"), nil
			}
			switch kind {
			case "explicit-error":
				want = "error"
				stage = "guest_explicit_error"
			case "missing":
				stage = "guest_decode_missing"
			case "malformed":
				stage = "guest_decode_malformed"
			case "length":
				stage = "guest_length"
			case "ok", "loss":
				want = "ok"
				stage = "guest_complete"
			}
			resp, _, err := b.Clipboard(ctx, req, []byte("ok"))
			if err != nil || resp.Status != want || fake.calls != 1 {
				t.Fatalf("original outcome/call count changed: %s %v %d", resp.Status, err, fake.calls)
			}
			found := false
			for _, event := range r.Fragment().Records {
				if event.Stage == stage {
					found = true
				}
			}
			if !found {
				raw, _ := json.Marshal(r.Fragment())
				t.Fatalf("actual branch %s absent: %s", stage, raw)
			}
			if kind == "loss" && r.Fragment().Complete {
				t.Fatal("postwrite diagnostic loss passed")
			}
		})
	}
}
func TestDiagnosticGuestForeignBindingAndRecorderLossRefuseBeforeExecutor(t *testing.T) {
	for _, kind := range []string{"foreign", "loss"} {
		t.Run(kind, func(t *testing.T) {
			b, req, fake, ctx, r := diagnosticGuestFixture(t)
			if kind == "foreign" {
				req.Generation = "00000000-0000-4000-8000-000000000005"
			} else {
				r.Invalidate()
			}
			if _, _, err := b.Clipboard(ctx, req, []byte("ok")); err == nil || fake.calls != 0 {
				t.Fatal("diagnostic preflight allowed dispatch")
			}
		})
	}
}

func TestDiagnosticGuestPredispatchActualFailureBranches(t *testing.T) {
	for _, kind := range []string{"request", "text", "read_payload", "context", "receiver", "executor", "prebinding"} {
		t.Run(kind, func(t *testing.T) {
			b, req, fake, ctx, r := diagnosticGuestFixture(t)
			payload := []byte("ok")
			switch kind {
			case "request":
				req.Version = 2
			case "text":
				payload = []byte{0xff}
			case "read_payload":
				req.Direction = "read"
			case "context":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "receiver":
				b = nil
			case "executor":
				b.ClipboardExecutor = nil
			case "prebinding":
				os.Remove(filepath.Join(b.Root, "run/boxwarden/clipboard-generation.json"))
			}
			_, _, err := b.Clipboard(ctx, req, payload)
			if err == nil || fake.calls != 0 {
				t.Fatal("predispatch branch dispatched")
			}
			records := r.Fragment().Records
			if len(records) != 1 || records[0].Stage != "guest_"+kind {
				t.Fatal("actual branch absent", kind, records)
			}
		})
	}
}
func TestDiagnosticGuestReadFailureBranchesAndPostBinding(t *testing.T) {
	for _, kind := range []string{"missing", "malformed", "execution", "context", "binding", "ok"} {
		t.Run(kind, func(t *testing.T) {
			b, req, fake, _, _ := diagnosticGuestFixture(t)
			req.Direction = "read"
			op := transportOperation()
			op.Direction = "read"
			op.ExpiresAt = req.ExpiresAt
			op.SessionID = req.SessionID
			op.BackendObject = req.BackendObject
			op.Generation = req.Generation
			r, _ := clipboarddiag.NewRecorder(op, "guest", time.Now)
			ctx, _ := clipboarddiag.WithContext(t.Context(), op, r)
			ctx, cancel := context.WithCancel(ctx)
			defer cancel()
			stage := "guest_complete"
			fake.fn = func() ([]byte, error) {
				switch kind {
				case "missing":
					return nil, nil
				case "malformed":
					return []byte("{}"), nil
				case "execution":
					return nil, errors.New("synthetic")
				case "context":
					cancel()
				case "binding":
					os.Remove(filepath.Join(b.Root, "run/boxwarden/clipboard-generation.json"))
				}
				return []byte("{\"version\":1,\"status\":\"ok\",\"length\":2}\nok"), nil
			}
			switch kind {
			case "missing":
				stage = "guest_decode_missing"
			case "malformed":
				stage = "guest_decode_malformed"
			case "execution":
				stage = "guest_execution"
			case "context":
				stage = "guest_post_context"
			case "binding":
				stage = "guest_postbinding"
			}
			_, data, err := b.Clipboard(ctx, req, nil)
			if fake.calls != 1 || (err == nil) != (kind == "ok") || (kind == "ok" && string(data) != "ok") {
				t.Fatal("original read outcome/count changed", err, fake.calls)
			}
			found := false
			for _, event := range r.Fragment().Records {
				if event.Stage == stage {
					found = true
				}
			}
			if !found {
				t.Fatal("actual read branch absent", stage, r.Fragment().Records)
			}
		})
	}
}
