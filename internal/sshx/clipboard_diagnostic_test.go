//go:build n1clipboarddiagnostic && !n1candidate

package sshx

import (
	"context"
	"errors"
	"github.com/weshofmann/boxwarden/internal/clipboarddiag"
	"github.com/weshofmann/boxwarden/internal/guestproto"
	"strings"
	"testing"
	"time"
)

func TestDiagnosticSSHActualTransportBranchAndFixedHelper(t *testing.T) {
	for _, kind := range []string{"ok", "missing", "malformed", "truncated", "length", "stdout", "stderr", "transport", "status-error", "status-unknown"} {
		t.Run(kind, func(t *testing.T) {
			conn := testConnection(t)
			conn.Binding.Domain = "n1qualification"
			conn.Pin.Domain = "n1qualification"
			req := clipboardRequest(conn, "write")
			op := clipboarddiag.Operation{Version: 1, OperationID: "00000000-0000-4000-8000-000000000001", Direction: req.Direction, Domain: req.Domain, SessionID: req.SessionID, BackendKind: req.BackendKind, BackendObject: req.BackendObject, Generation: req.Generation, ExpiresAt: req.ExpiresAt}
			r, _ := clipboarddiag.NewRecorder(op, "supervisor", time.Now)
			ctx, _ := clipboarddiag.WithContext(context.Background(), op, r)
			runner := &fakeRunner{onRun: func(cmd Command) Result {
				switch kind {
				case "missing":
					return Result{}
				case "malformed":
					return Result{Stdout: "{}"}
				case "stdout":
					return Result{Stdout: strings.Repeat("x", guestproto.MaxClipboardResponseBytes+1)}
				case "stderr":
					return Result{Stderr: strings.Repeat("x", guestproto.MaxClipboardMetadataBytes+1)}
				case "status-error":
					return Result{Stdout: responseFrame(t, req, "error", nil, 0)}
				case "status-unknown":
					return Result{Stdout: responseFrame(t, req, "unknown", nil, 0)}
				case "truncated":
					return Result{Truncated: true}
				case "length":
					return Result{Stdout: responseFrame(t, req, "ok", nil, 2)}
				}
				return Result{Stdout: responseFrame(t, req, "ok", nil, 1)}
			}}
			if kind == "transport" {
				runner.err = errors.New("synthetic private error")
			}
			_, _, _ = NewClientWithClipboardRunner(nil, runner).Clipboard(ctx, conn, req, []byte("x"))
			if len(runner.commands) != 1 {
				t.Fatal("dispatch count changed")
			}
			cmd := runner.commands[0]
			if cmd.Args[len(cmd.Args)-2] != guestproto.ClipboardDiagnosticHelperPath || cmd.Args[len(cmd.Args)-1] != "invoke" {
				t.Fatal("fixed diagnostic helper missing")
			}
			stage := map[string]string{"ok": "ssh_complete", "missing": "ssh_ack_missing", "truncated": "ssh_truncated", "length": "ssh_ack_length", "malformed": "ssh_ack_malformed", "stdout": "ssh_stdout_bound", "stderr": "ssh_stderr_bound", "transport": "ssh_transport", "status-error": "ssh_ack_status", "status-unknown": "ssh_ack_status"}[kind]
			found := false
			for _, e := range r.Fragment().Records {
				if e.Stage == stage {
					found = true
				}
			}
			if !found {
				t.Fatal("actual branch absent", stage)
			}
		})
	}
}
func TestDiagnosticSSHRecorderOverflowRejectsBeforeRunner(t *testing.T) {
	conn := testConnection(t)
	conn.Binding.Domain = "n1qualification"
	conn.Pin.Domain = "n1qualification"
	req := clipboardRequest(conn, "write")
	op := clipboarddiag.Operation{Version: 1, OperationID: "00000000-0000-4000-8000-000000000001", Direction: req.Direction, Domain: req.Domain, SessionID: req.SessionID, BackendKind: req.BackendKind, BackendObject: req.BackendObject, Generation: req.Generation, ExpiresAt: req.ExpiresAt}
	r, _ := clipboarddiag.NewRecorder(op, "supervisor", time.Now)
	ctx, _ := clipboarddiag.WithContext(context.Background(), op, r)
	for _, entry := range [][2]string{{"runtime", "runtime_client_missing"}, {"runtime", "runtime_final_context"}, {"runtime", "runtime_context"}, {"runtime", "runtime_owner"}, {"runtime", "runtime_client"}, {"runtime", "runtime_observation_context"}, {"runtime", "runtime_ready"}, {"runtime", "runtime_connection"}, {"runtime", "runtime_binding"}, {"runtime", "runtime_record"}, {"runtime", "runtime_admitted"}, {"runtime", "runtime_request"}, {"runtime", "runtime_dispatch"}, {"runtime", "runtime_transport"}, {"runtime", "runtime_response"}, {"runtime", "runtime_length"}, {"runtime", "runtime_post_context"}, {"runtime", "runtime_post_binding"}, {"runtime", "runtime_post_ready"}, {"runtime", "runtime_post_connection"}, {"runtime", "runtime_post_record"}, {"runtime", "runtime_status"}, {"runtime", "runtime_text"}, {"runtime", "runtime_complete"}, {"supervisor", "control_expiry_missing"}, {"supervisor", "control_expiry_elapsed"}, {"supervisor", "control_expiry_bound"}, {"supervisor", "control_expiry"}, {"supervisor", "control_deadline"}, {"supervisor", "control_capability"}, {"supervisor", "control_context"}, {"supervisor", "control_binding"}} {
		clipboarddiag.Record(clipboarddiag.WithSource(ctx, entry[0]), entry[1], "ok")
	}
	if !clipboarddiag.Available(ctx) {
		t.Fatal("quota fixture lost metadata before reaching32 valid records")
	}
	runner := &fakeRunner{}
	_, _, err := NewClientWithClipboardRunner(nil, runner).Clipboard(ctx, conn, req, []byte("synthetic"))
	if err == nil || len(runner.commands) != 0 || r.Fragment().Complete {
		t.Fatal("dispatch followed diagnostic recorder overflow")
	}
}
