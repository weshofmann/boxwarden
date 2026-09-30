//go:build n1clipboarddiagnostic && !n1candidate

package sessionruntime

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/weshofmann/boxwarden/internal/clipboarddiag"
	"github.com/weshofmann/boxwarden/internal/clipboardx"
	"github.com/weshofmann/boxwarden/internal/guestproto"
	"github.com/weshofmann/boxwarden/internal/hostx"
	"github.com/weshofmann/boxwarden/internal/session"
	"github.com/weshofmann/boxwarden/internal/sshx"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDiagnosticOwnerCancelledBranchIsRecordedWithoutGuestCall(t *testing.T) {
	op := clipboarddiag.Operation{Version: 1, OperationID: "00000000-0000-4000-8000-000000000001", Direction: "read", Domain: "n1qualification", SessionID: "00000000-0000-4000-8000-000000000002", BackendKind: "tart", BackendObject: "synthetic", Generation: "00000000-0000-4000-8000-000000000003", ExpiresAt: time.Now().UTC().Add(time.Second)}
	r, _ := clipboarddiag.NewRecorder(op, "supervisor", time.Now)
	ctx, _ := clipboarddiag.WithContext(context.Background(), op, r)
	ctx, cancel := context.WithCancel(ctx)
	cancel()
	var owner *Owner
	_, _ = owner.ReadClipboard(ctx)
	events := r.Fragment().Records
	if len(events) != 1 || events[0].Stage != "runtime_context" {
		t.Fatal("actual context short-circuit unrecorded")
	}
}

// This fixture exercises the actual owner admission path with synthetic state,
// retained handle, serial, pinned-key store and management client only.
func readyDiagnosticClipboardFixture(t *testing.T) (*fixture, *ownerClipboardClient, context.Context, *clipboarddiag.Recorder) {
	t.Helper()
	f, ready := readyFixture(t)
	f.record.Domain = "n1qualification"
	f.request.Binding.Domain = "n1qualification"
	f.request.RuntimeDirectory = filepath.Join(f.root, "runtime", "n1qualification", f.record.ID, f.record.StartGeneration)
	f.serial.endpoint = filepath.Join(f.request.RuntimeDirectory, "serial", "tart-serial")
	if os.MkdirAll(f.request.RuntimeDirectory, 0700) != nil {
		t.Fatal("synthetic runtime")
	}
	var config map[string]any
	raw, _ := os.ReadFile(f.request.HostConfigPath)
	if json.Unmarshal(raw, &config) != nil {
		t.Fatal("synthetic config")
	}
	domains := config["domains"].(map[string]any)
	config["domains"] = map[string]any{"n1qualification": domains["work"]}
	raw, _ = json.Marshal(config)
	if os.WriteFile(f.request.HostConfigPath, raw, 0600) != nil {
		t.Fatal("synthetic config write")
	}
	if session.SaveRecord(f.root, f.record.Domain, f.record) != nil {
		t.Fatal("synthetic record")
	}
	f.owner.deps.host = hostFunc(func(context.Context, hostx.Request) (hostx.RuntimeExpectation, error) {
		return hostx.RuntimeExpectation{Manifest: hostx.Manifest{Tart: hostx.ToolIdentity{Path: f.tartPath}, TartHome: f.tartHome, Operator: hostx.Operator{Home: "/admitted/operator", Name: "admitted-operator"}}, SoftnetBinDir: "/qualified/softnet/bin"}, nil
	})
	f.owner.deps.ca = caFunc(func(context.Context, sshx.Domain, []sshx.Domain) (sshx.CAIdentity, error) {
		return sshx.CAIdentity{Domain: "n1qualification", Algorithm: "ssh-ed25519", PublicKey: ownerTestPublicKey, Fingerprint: ownerTestFingerprint(), PrivateKeyPath: filepath.Join(f.root, "identity", "ssh-user-ca", "ca")}, nil
	})
	for _, run := range []func(context.Context) error{func(ctx context.Context) error { return f.owner.Start(ctx, f.request) }, f.owner.Bootstrap, f.owner.Ready} {
		if err := run(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { _ = f.owner.Stop(context.Background()); _ = f.owner.Wait(context.Background()) })
	f.record.IntendedState = session.StateRunning
	f.record.Readiness = session.ReadinessRecord{Status: session.ReadinessReady}
	if session.SaveRecord(f.root, f.record.Domain, f.record) != nil {
		t.Fatal("synthetic ready record")
	}
	client := &ownerClipboardClient{readyClient: ready}
	client.run = func(_ context.Context, _ sshx.Connection, req guestproto.ClipboardRequest, data []byte) (guestproto.ClipboardResponse, []byte, error) {
		return guestproto.ClipboardResponse{Version: guestproto.Version, Association: req.Association, Generation: req.Generation, Status: "ok", Length: len(data)}, nil, nil
	}
	f.owner.deps.client = client
	b := f.request.Binding
	op := clipboarddiag.Operation{Version: 1, OperationID: "00000000-0000-4000-8000-000000000001", Direction: "write", Domain: b.Domain, SessionID: b.SessionID, BackendKind: b.BackendKind, BackendObject: b.BackendObject, Generation: b.Generation, ExpiresAt: time.Now().UTC().Add(5 * time.Second)}
	r, err := clipboarddiag.NewRecorder(op, "supervisor", time.Now)
	if err != nil {
		t.Fatal(err)
	}
	ctx, _ := clipboarddiag.WithContext(context.Background(), op, r)
	ctx, cancel := context.WithDeadline(ctx, op.ExpiresAt)
	t.Cleanup(cancel)
	return f, client, ctx, r
}
func TestDiagnosticOwnerActualAdmissionAndPostDispatchBranches(t *testing.T) {
	for _, kind := range []string{"ready", "record", "connection", "explicit-error", "unknown", "transport", "safe-transport", "response", "length", "post-ready", "post-record", "post-context", "loss", "ok"} {
		t.Run(kind, func(t *testing.T) {
			f, c, ctx, r := readyDiagnosticClipboardFixture(t)
			ctx, cancelOperation := context.WithCancel(ctx)
			defer cancelOperation()
			want := clipboardx.Unknown
			stage := "runtime_transport"
			calls := 1
			switch kind {
			case "ready":
				c.probe = func(sshx.Connection) error { return errors.New("synthetic") }
				want = clipboardx.Unchanged
				stage = "runtime_ready"
				calls = 0
			case "record":
				f.record.StartGeneration = "00000000-0000-4000-8000-000000000009"
				if session.SaveRecord(f.root, f.record.Domain, f.record) != nil {
					t.Fatal("record")
				}
				want = clipboardx.Unchanged
				stage = "runtime_record"
				calls = 0
			case "connection":
				c.probe = func(sshx.Connection) error {
					f.owner.mu.Lock()
					f.owner.connection.RuntimeDirectory = "synthetic-other"
					f.owner.mu.Unlock()
					return nil
				}
				want = clipboardx.Unchanged
				stage = "runtime_connection"
				calls = 0
			default:
				original := c.run
				c.run = func(operationCtx context.Context, conn sshx.Connection, req guestproto.ClipboardRequest, data []byte) (guestproto.ClipboardResponse, []byte, error) {
					response, payload, err := original(operationCtx, conn, req, data)
					switch kind {
					case "explicit-error":
						response.Status = "error"
						response.Length = 0
					case "unknown":
						response.Status = "unknown"
						response.Length = 0
					case "transport":
						return response, nil, errors.New("synthetic private error")
					case "safe-transport":
						return response, nil, clipboardx.ErrAdmission
					case "response":
						response.Generation = "00000000-0000-4000-8000-000000000009"
					case "length":
						response.Length++
					case "post-ready":
						c.probe = func(sshx.Connection) error { return errors.New("synthetic") }
					case "post-record":
						f.record.StartGeneration = "00000000-0000-4000-8000-000000000009"
						if session.SaveRecord(f.root, f.record.Domain, f.record) != nil {
							t.Fatal("record")
						}
					case "post-context":
						cancelOperation()
					case "loss":
						r.Invalidate()
					}
					return response, payload, err
				}
			}
			switch kind {
			case "explicit-error":
				want = clipboardx.Unchanged
				stage = "runtime_status"
			case "unknown":
				stage = "runtime_status"
			case "safe-transport":
				want = clipboardx.Unchanged
			case "response":
				stage = "runtime_response"
			case "length":
				stage = "runtime_length"
			case "post-ready":
				stage = "runtime_post_ready"
			case "post-record":
				stage = "runtime_post_record"
			case "post-context":
				stage = "runtime_post_context"
			case "ok", "loss":
				want = clipboardx.Committed
				stage = "runtime_complete"
			}

			outcome, _ := f.owner.WriteClipboard(ctx, []byte("synthetic"))
			if outcome != want || c.calls != calls {
				t.Fatalf("outcome/calls %s/%d expected %s/%d", outcome, c.calls, want, calls)
			}
			found := false
			for _, event := range r.Fragment().Records {
				if event.Stage == stage {
					found = true
				}
			}
			if !found {
				t.Fatal("actual branch absent", stage, r.Fragment().Records)
			}
			if kind == "loss" && r.Fragment().Complete {
				t.Fatal("postdispatch loss promoted")
			}
		})
	}
}
