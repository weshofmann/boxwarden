package supervisor

import (
	"context"
	"encoding/json"
	"net"
	"testing"
	"time"

	"github.com/weshofmann/boxwarden/internal/clipboardx"
)

type clipboardTestOwner struct {
	binding Binding
	ready   bool
	calls   int
	data    []byte
	outcome clipboardx.Outcome
	err     error
}

func (o *clipboardTestOwner) Snapshot(context.Context) Snapshot {
	return Snapshot{Binding: o.binding, BackendRunning: o.ready, SerialHealthy: o.ready, PinPresent: o.ready, CertificateCurrent: o.ready, ProbeOK: o.ready, ZoneMatches: o.ready}
}
func (o *clipboardTestOwner) Start(context.Context, LaunchRequest) error { return nil }
func (o *clipboardTestOwner) Stop(context.Context) error                 { return nil }
func (o *clipboardTestOwner) Wait(context.Context) error                 { return nil }
func (o *clipboardTestOwner) Bootstrap(context.Context) error            { return nil }
func (o *clipboardTestOwner) Ready(context.Context) error                { return nil }
func (o *clipboardTestOwner) ReadClipboard(ctx context.Context) ([]byte, error) {
	o.calls++
	return o.data, o.err
}
func (o *clipboardTestOwner) WriteClipboard(ctx context.Context, data []byte) (clipboardx.Outcome, error) {
	o.calls++
	o.data = append([]byte(nil), data...)
	return o.outcome, o.err
}
func TestClipboardHandshakeDoesNotReadSourceOrDispatchUntilAuthorized(t *testing.T) {
	b := minimalRequest(t).Binding
	o := &clipboardTestOwner{binding: b, ready: true, outcome: clipboardx.Committed}
	server, client := net.Pipe()
	done := make(chan struct{})
	go func() { handleControl(context.Background(), server, b, o, func() error { return nil }); close(done) }()
	defer client.Close()
	client.SetDeadline(time.Now().Add(time.Second))
	req := controlRequest{Version: 1, Binding: b, Action: "clipboard_write", ExpiresAt: time.Now().Add(time.Second)}
	data, _ := json.Marshal(req)
	if err := writeFrame(client, data); err != nil {
		t.Fatal(err)
	}
	header, err := readBounded(client)
	if err != nil {
		t.Fatal(err)
	}
	var ready clipboardControlReply
	if err := decodeExact(header, &ready); err != nil || ready.Status != "ready" || ready.Binding != b {
		t.Fatal("no exact readiness handshake", err)
	}
	if o.calls != 0 {
		t.Fatal("guest called before payload authorization")
	}
	client.Close()
	<-done
	if o.calls != 0 {
		t.Fatal("disconnect before authorization called guest")
	}
}
func TestClipboardNotReadyNeverDispatches(t *testing.T) {
	b := minimalRequest(t).Binding
	o := &clipboardTestOwner{binding: b}
	server, client := net.Pipe()
	done := make(chan struct{})
	go func() { handleControl(context.Background(), server, b, o, func() error { return nil }); close(done) }()
	client.SetDeadline(time.Now().Add(time.Second))
	req := controlRequest{Version: 1, Binding: b, Action: "clipboard_read", ExpiresAt: time.Now().Add(time.Second)}
	data, _ := json.Marshal(req)
	writeFrame(client, data)
	header, err := readBounded(client)
	if err != nil {
		t.Fatal(err)
	}
	var reply clipboardControlReply
	if decodeExact(header, &reply) != nil || reply.Status != "error" {
		t.Fatal("notready accepted")
	}
	client.Close()
	<-done
	if o.calls != 0 {
		t.Fatal("notready read guest")
	}
}
func TestClipboardControlRejectsUnrelatedActionFields(t *testing.T) {
	for _, action := range []string{"clipboard_read", "clipboard_write"} {
		if !validControlAction(controlRequest{Action: action}) {
			t.Fatal("clipboard capability unavailable")
		}
		if validControlAction(controlRequest{Action: action, Packages: []string{"secret"}}) {
			t.Fatal("unrelated fields accepted")
		}
	}
}

func TestClipboardWriteAcknowledgementAndReadBytes(t *testing.T) {
	for _, direction := range []clipboardx.Direction{clipboardx.ToGuest, clipboardx.FromGuest} {
		t.Run(string(direction), func(t *testing.T) {
			b := minimalRequest(t).Binding
			value := []byte("λ\n\t🙂\n\n")
			o := &clipboardTestOwner{binding: b, ready: true, outcome: clipboardx.Committed, data: value}
			server, client := net.Pipe()
			done := make(chan struct{})
			go func() { handleControl(context.Background(), server, b, o, func() error { return nil }); close(done) }()
			defer client.Close()
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			client.SetDeadline(time.Now().Add(time.Second))
			action := "clipboard_read"
			if direction == clipboardx.ToGuest {
				action = "clipboard_write"
			}
			data, _ := json.Marshal(controlRequest{Version: 1, Binding: b, Action: action, ExpiresAt: time.Now().Add(time.Second)})
			if err := writeFrame(client, data); err != nil {
				t.Fatal(err)
			}
			if _, err := readBounded(client); err != nil {
				t.Fatal(err)
			}
			transfer := &clipboardConnection{conn: client, binding: b, direction: direction, ctx: ctx, cancel: cancel}
			if direction == clipboardx.ToGuest {
				outcome, err := transfer.Write(ctx, value)
				if err != nil || outcome != clipboardx.Committed {
					t.Fatalf("write %s %v", outcome, err)
				}
			} else {
				got, err := transfer.Read(ctx)
				if err != nil || string(got) != string(value) {
					t.Fatalf("read bytes mismatched %v", err)
				}
			}
			transfer.Close()
			<-done
			if o.calls != 1 {
				t.Fatal("operation replayed or not dispatched")
			}
		})
	}
}
func TestClipboardWriteErrorIsSanitizedAndUnknownNotReplayed(t *testing.T) {
	b := minimalRequest(t).Binding
	o := &clipboardTestOwner{binding: b, ready: true, outcome: clipboardx.Unknown, err: clipboardx.ErrUnknown}
	server, client := net.Pipe()
	done := make(chan struct{})
	go func() { handleControl(context.Background(), server, b, o, func() error { return nil }); close(done) }()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	client.SetDeadline(time.Now().Add(time.Second))
	data, _ := json.Marshal(controlRequest{Version: 1, Binding: b, Action: "clipboard_write", ExpiresAt: time.Now().Add(time.Second)})
	writeFrame(client, data)
	if _, err := readBounded(client); err != nil {
		t.Fatal(err)
	}
	transfer := &clipboardConnection{conn: client, binding: b, direction: clipboardx.ToGuest, ctx: ctx, cancel: cancel}
	outcome, err := transfer.Write(ctx, []byte("synthetic"))
	if outcome != clipboardx.Unknown || err != clipboardx.ErrUnknown {
		t.Fatal("ambiguous write claimed rollback")
	}
	if second, _ := transfer.Write(ctx, []byte("next")); second != clipboardx.Unchanged {
		t.Fatal("used transfer replayed")
	}
	transfer.Close()
	<-done
	if o.calls != 1 {
		t.Fatal("ambiguous write replayed")
	}
}

type cancellableClipboardOwner struct {
	clipboardTestOwner
	started   chan struct{}
	cancelled chan struct{}
}

func (o *cancellableClipboardOwner) ReadClipboard(ctx context.Context) ([]byte, error) {
	close(o.started)
	<-ctx.Done()
	close(o.cancelled)
	return nil, clipboardx.ErrCancelled
}
func TestClipboardDisconnectCancelsRetainedGuestRead(t *testing.T) {
	b := minimalRequest(t).Binding
	o := &cancellableClipboardOwner{clipboardTestOwner: clipboardTestOwner{binding: b, ready: true}, started: make(chan struct{}), cancelled: make(chan struct{})}
	server, client := net.Pipe()
	done := make(chan struct{})
	go func() { handleControl(context.Background(), server, b, o, func() error { return nil }); close(done) }()
	client.SetDeadline(time.Now().Add(time.Second))
	data, _ := json.Marshal(controlRequest{Version: 1, Binding: b, Action: "clipboard_read", ExpiresAt: time.Now().Add(time.Second)})
	writeFrame(client, data)
	if _, err := readBounded(client); err != nil {
		t.Fatal(err)
	}
	client.Write([]byte{0})
	select {
	case <-o.started:
	case <-time.After(time.Second):
		t.Fatal("guest read not dispatched")
	}
	client.Close()
	select {
	case <-o.cancelled:
	case <-time.After(time.Second):
		t.Fatal("disconnected guest read still running")
	}
	<-done
}
