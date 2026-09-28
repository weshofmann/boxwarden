package sshx

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/weshofmann/boxwarden/internal/clipboardx"
	"github.com/weshofmann/boxwarden/internal/guestproto"
	"strings"
	"testing"
	"time"
)

func clipboardRequest(conn Connection, direction string) guestproto.ClipboardRequest {
	return guestproto.ClipboardRequest{Version: guestproto.Version, Association: guestproto.Association{Domain: string(conn.Binding.Domain), SessionID: conn.Binding.SessionID, BackendKind: conn.Binding.BackendKind, BackendObject: conn.Binding.BackendObject}, Generation: "00000000-0000-4000-8000-000000000003", Direction: direction, ExpiresAt: time.Now().Add(29 * time.Second).UTC()}
}
func responseFrame(t *testing.T, req guestproto.ClipboardRequest, status string, data []byte, length int) string {
	t.Helper()
	response := guestproto.ClipboardResponse{Version: guestproto.Version, Association: req.Association, Generation: req.Generation, Status: status, Length: length}
	raw, err := guestproto.EncodeClipboardResponse(req, response, data)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}
func TestClipboardUsesStrictFixedHelperAndPayloadStdin(t *testing.T) {
	conn := testConnection(t)
	req := clipboardRequest(conn, "write")
	payload := []byte("雪\n\n")
	runner := &fakeRunner{onRun: func(Command) Result { return Result{Stdout: responseFrame(t, req, "ok", nil, len(payload))} }}
	management := &fakeRunner{}
	response, data, err := NewClientWithClipboardRunner(management, runner).Clipboard(context.Background(), conn, req, payload)
	if err != nil || response.Status != "ok" || len(data) != 0 || len(runner.commands) != 1 || len(management.commands) != 0 {
		t.Fatalf("clipboard fixed exchange failed: %v", err)
	}
	command := runner.commands[0]
	want := expectedSSHArgs(conn)
	want[len(want)-1] = "clipboard"
	if command.Path != sshPath || !sameStrings(command.Args, want) {
		t.Fatal("strict fixed clipboard argv changed")
	}
	gotReq, gotPayload, err := guestproto.DecodeClipboardRequest(bytes.NewReader(command.Stdin))
	if err != nil || gotReq != req || !bytes.Equal(gotPayload, payload) {
		t.Fatal("payload or binding altered")
	}
	for _, arg := range command.Args {
		if strings.Contains(arg, "雪") {
			t.Fatal("payload in argv")
		}
	}
}
func TestClipboardDedicatedRunnerSupportsBoundedMiBWithoutGenericExpansion(t *testing.T) {
	payload := bytes.Repeat([]byte("a"), 1048576)
	command := Command{Path: "/bin/cat", Stdin: payload}
	got, err := NewClipboardExecRunner().Run(context.Background(), command)
	if err != nil || got.Truncated || !bytes.Equal([]byte(got.Stdout), payload) {
		t.Fatalf("clipboard runner failed bounded payload: %v", err)
	}
	if _, err := NewExecRunner().Run(context.Background(), command); err == nil {
		t.Fatal("generic stdin limit expanded")
	}
	command.Stdin = bytes.Repeat([]byte("a"), guestproto.MaxClipboardRequestBytes+1)
	if _, err := NewClipboardExecRunner().Run(context.Background(), command); err == nil {
		t.Fatal("clipboard stdin unbounded")
	}
}
func TestClipboardRejectsPredispatchBindingPinTextAndCancellation(t *testing.T) {
	for _, kind := range []string{"binding", "pin", "text", "cancel"} {
		t.Run(kind, func(t *testing.T) {
			conn := testConnection(t)
			req := clipboardRequest(conn, "write")
			payload := []byte("value")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch kind {
			case "binding":
				req.BackendObject = "other"
			case "pin":
				mustWrite(t, conn.KnownHostsFile, []byte(HostKeyAlias(conn.Binding.SessionID)+" "+changedPublicKey+"\n"), 0600)
			case "text":
				payload = []byte{0xff}
			case "cancel":
				cancel()
			}
			runner := &fakeRunner{}
			_, _, err := NewClientWithClipboardRunner(nil, runner).Clipboard(ctx, conn, req, payload)
			if err == nil || errors.Is(err, clipboardx.ErrUnknown) || len(runner.commands) != 0 {
				t.Fatalf("unsafe predispatch: %v", err)
			}
		})
	}
}
func TestClipboardMaliciousResponsesBecomeUnknownAfterWriteDispatch(t *testing.T) {
	conn := testConnection(t)
	req := clipboardRequest(conn, "write")
	raw := responseFrame(t, req, "ok", nil, 1)
	for name, result := range map[string]Result{"association": {Stdout: strings.Replace(raw, req.BackendObject, "other", 1)}, "generation": {Stdout: strings.Replace(raw, req.Generation, "00000000-0000-4000-8000-000000000004", 1)}, "length": {Stdout: strings.Replace(raw, `"length":1`, `"length":2`, 1)}, "trailing": {Stdout: raw + "x"}, "oversize": {Stdout: strings.Repeat("a", guestproto.MaxClipboardResponseBytes+1)}, "truncated": {Stdout: raw, Truncated: true}} {
		t.Run(name, func(t *testing.T) {
			runner := &fakeRunner{onRun: func(Command) Result { return result }}
			_, _, err := NewClientWithClipboardRunner(nil, runner).Clipboard(context.Background(), conn, req, []byte("a"))
			if !errors.Is(err, clipboardx.ErrUnknown) || len(runner.commands) != 1 {
				t.Fatalf("unsafe write classification: %v", err)
			}
		})
	}
}
func TestClipboardReadValidatesExactCompleteText(t *testing.T) {
	conn := testConnection(t)
	req := clipboardRequest(conn, "read")
	for _, payload := range [][]byte{{}, []byte("雪\n\n"), bytes.Repeat([]byte("a"), 1048576)} {
		runner := &fakeRunner{onRun: func(Command) Result { return Result{Stdout: responseFrame(t, req, "ok", payload, len(payload))} }}
		response, got, err := NewClientWithClipboardRunner(nil, runner).Clipboard(context.Background(), conn, req, nil)
		if err != nil || response.Status != "ok" || !bytes.Equal(got, payload) {
			t.Fatalf("read text changed: %v", err)
		}
	}
	for _, payload := range [][]byte{{0xff}, {0}, bytes.Repeat([]byte("a"), 1048577)} {
		response := guestproto.ClipboardResponse{Version: guestproto.Version, Association: req.Association, Generation: req.Generation, Status: "ok", Length: len(payload)}
		head, _ := json.Marshal(response)
		raw := append(append(head, '\n'), payload...)
		runner := &fakeRunner{onRun: func(Command) Result { return Result{Stdout: string(raw)} }}
		_, got, err := NewClientWithClipboardRunner(nil, runner).Clipboard(context.Background(), conn, req, nil)
		if !errors.Is(err, clipboardx.ErrRead) || len(got) != 0 {
			t.Fatalf("invalid read accepted: %v", err)
		}
	}
}

type clipboardDeadlineRunner struct {
	deadline time.Time
	err      error
}

func (r *clipboardDeadlineRunner) Run(ctx context.Context, _ Command) (Result, error) {
	r.deadline, _ = ctx.Deadline()
	return Result{}, r.err
}
func TestClipboardDispatchErrorsAreSanitizedAndBounded(t *testing.T) {
	conn := testConnection(t)
	for _, direction := range []string{"read", "write"} {
		req := clipboardRequest(conn, direction)
		runner := &clipboardDeadlineRunner{err: errors.New("synthetic private payload")}
		_, _, err := NewClientWithClipboardRunner(nil, runner).Clipboard(context.Background(), conn, req, nil)
		want := clipboardx.ErrRead
		if direction == "write" {
			want = clipboardx.ErrUnknown
		}
		if err != want || time.Until(runner.deadline) > 30*time.Second || time.Until(runner.deadline) <= 0 {
			t.Fatalf("dispatch error or timeout: %v", err)
		}
	}
}
