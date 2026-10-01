//go:build n1clipboarddiagnostic && !n1candidate

package sshx

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/weshofmann/boxwarden/internal/clipboarddiag"
	"github.com/weshofmann/boxwarden/internal/clipboardx"
	"github.com/weshofmann/boxwarden/internal/execx"
	"strings"
	"testing"
	"time"
)

func TestDiagnosticInvokeWitnessBeforeACKAndMetadataLossPreservesOutcome(t *testing.T) {
	for _, kind := range []string{"valid", "malformed-ack", "missing", "bad-json", "duplicate", "trailing", "oversize", "truncated", "read-close", "transport", "wrong-expiry", "wrong-phase"} {
		t.Run(kind, func(t *testing.T) {
			conn := testConnection(t)
			conn.Binding.Domain = "n1qualification"
			conn.Pin.Domain = "n1qualification"
			req := clipboardRequest(conn, "write")
			op := clipboarddiag.Operation{Version: 1, OperationID: "00000000-0000-4000-8000-000000000001", Direction: req.Direction, Domain: req.Domain, SessionID: req.SessionID, BackendKind: req.BackendKind, BackendObject: req.BackendObject, Generation: req.Generation, ExpiresAt: req.ExpiresAt}
			r, _ := clipboarddiag.NewRecorder(op, "supervisor", time.Now)
			ctx, _ := clipboarddiag.WithContext(t.Context(), op, r)
			seen := 0
			ctx = clipboarddiag.WithPublicationObserver(ctx, func(w clipboarddiag.PublicationWitness) {
				seen++
				if w.Header != clipboarddiag.HeaderDigest(op) {
					t.Fatal("wrong operation observed")
				}
			})
			h := strings.Repeat("a", 64)
			w := clipboarddiag.PublicationWitness{V: 1, Phase: "invoke", Header: clipboarddiag.HeaderDigest(op), Generation: clipboarddiag.GenerationDigest(op), Created: true, Bootstrap: &h, Closure: &h}
			proof, _ := clipboarddiag.EncodePublication(w)
			result := Result{Stdout: responseFrame(t, req, "ok", nil, 1), Stderr: string(proof), StderrComplete: true}
			switch kind {
			case "malformed-ack":
				result.Stdout = "{}"
			case "missing":
				result.Stderr = ""
			case "bad-json":
				result.Stderr = "{}\n"
			case "duplicate":
				result.Stderr = strings.Replace(result.Stderr, `"v":1`, `"v":1,"v":1`, 1)
			case "trailing":
				result.Stderr += "x"
			case "oversize":
				result.Stderr = strings.Repeat("m", 513)
			case "truncated":
				result.Stderr = strings.Repeat("m", 512)
				result.StderrComplete = false
				result.Truncated = true
				result.StderrTruncated = true
			case "read-close":
				result.StderrComplete = false
			case "wrong-expiry":
				other := op
				other.ExpiresAt = other.ExpiresAt.Add(time.Nanosecond)
				w.Header = clipboarddiag.HeaderDigest(other)
				proof, _ = clipboarddiag.EncodePublication(w)
				result.Stderr = string(proof)
			case "wrong-phase":
				w.Phase = "collect"
				w.Created = false
				w.Bootstrap = nil
				w.Closure = nil
				w.Collect = &h
				proof, _ = clipboarddiag.EncodePublication(w)
				result.Stderr = string(proof)
			}
			runner := &fakeRunner{onRun: func(Command) Result { return result }}
			if kind == "transport" {
				runner.err = errors.New("private transport failure")
			}
			response, _, err := NewClientWithClipboardRunner(nil, runner).Clipboard(ctx, conn, req, []byte("x"))
			goodProof := kind == "valid" || kind == "malformed-ack"
			if (seen == 1) != goodProof || len(runner.commands) != 1 {
				t.Fatal("proof capture/count", seen)
			}
			if kind == "malformed-ack" || kind == "transport" {
				if !errors.Is(err, clipboardx.ErrUnknown) {
					t.Fatal("original unknown outcome changed", err)
				}
			} else if err != nil || response.Status != "ok" {
				t.Fatal("metadata loss replaced authoritative ACK", err)
			}
		})
	}
}
func TestDiagnosticInvokeProductionFactoryFixedCaps(t *testing.T) {
	runner := newClipboardDiagnosticInvokeRunner().runner.(execx.OSRunner)
	if !runner.StrictStderr || runner.MaxStderrBytes != 512 || runner.MaxStdoutBytes <= 32768 || runner.MaxStdinBytes <= 32768 {
		t.Fatal("fixed invoke caps")
	}
	client := NewClient(nil)
	if client.clipboardDiagnosticRunner == nil {
		t.Fatal("production diagnostic runner absent")
	}
	ctx := context.Background()
	if clipboardExchangeRunner(ctx, client) != client.clipboardRunner {
		t.Fatal("ordinary runner changed")
	}
}

func TestDiagnosticCollectWitnessStrictAdmissionAndOneCall(t *testing.T) {
	for _, kind := range []string{"valid", "missing-after-effect", "duplicate", "trailing", "oversize", "truncated", "no-eof", "transport", "wrong-expiry", "wrong-generation", "wrong-marker", "wrong-phase"} {
		t.Run(kind, func(t *testing.T) {
			conn := testConnection(t)
			conn.Binding.Domain = "n1qualification"
			conn.Pin.Domain = "n1qualification"
			req := clipboardRequest(conn, "read")
			op := clipboarddiag.Operation{Version: 1, OperationID: "00000000-0000-4000-8000-000000000001", Direction: req.Direction, Domain: req.Domain, SessionID: req.SessionID, BackendKind: req.BackendKind, BackendObject: req.BackendObject, Generation: req.Generation, ExpiresAt: req.ExpiresAt}
			digest := clipboarddiag.MetadataDigest([]byte(clipboarddiag.HeaderDigest(op) + "\n"))
			w := clipboarddiag.PublicationWitness{V: 1, Phase: "collect", Header: clipboarddiag.HeaderDigest(op), Generation: clipboarddiag.GenerationDigest(op), Collect: &digest}
			proof, _ := clipboarddiag.EncodePublication(w)
			raw, _ := json.Marshal(clipboarddiag.GuestCollection{Version: 1, Binding: op})
			result := Result{Stdout: string(raw) + "\n", Stderr: string(proof), StderrComplete: true}
			switch kind {
			case "missing-after-effect":
				result.Stderr = ""
			case "duplicate":
				result.Stderr = strings.Replace(result.Stderr, `"v":1`, `"v":1,"v":1`, 1)
			case "trailing":
				result.Stderr += "x"
			case "oversize":
				result.Stderr = strings.Repeat("m", 513)
			case "truncated":
				result.Truncated = true
				result.StderrTruncated = true
				result.StderrComplete = false
			case "no-eof":
				result.StderrComplete = false
			case "wrong-expiry":
				other := op
				other.ExpiresAt = other.ExpiresAt.Add(time.Nanosecond)
				w.Header = clipboarddiag.HeaderDigest(other)
			case "wrong-generation":
				w.Generation = strings.Repeat("a", 64)
			case "wrong-marker":
				digest = strings.Repeat("a", 64)
			case "wrong-phase":
				w.Phase = "invoke"
				w.Created = true
				w.Bootstrap = &digest
				w.Closure = &digest
				w.Collect = nil
			}
			if strings.HasPrefix(kind, "wrong-") {
				proof, _ = clipboarddiag.EncodePublication(w)
				result.Stderr = string(proof)
			}
			runner := &fakeRunner{onRun: func(Command) Result { return result }}
			if kind == "transport" {
				runner.err = errors.New("private transport failure")
			}
			ctx, cancel := context.WithDeadline(t.Context(), op.ExpiresAt)
			defer cancel()
			_, err := NewClientWithClipboardRunner(nil, runner).collectClipboardDiagnosticWith(ctx, conn, op, runner)
			if (err == nil) != (kind == "valid") || len(runner.commands) != 1 {
				t.Fatal("collect witness admission/one call", err, len(runner.commands))
			}
		})
	}
}
