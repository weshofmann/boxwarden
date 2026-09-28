package sessionruntime

import (
	"bytes"
	"context"
	"errors"
	"github.com/weshofmann/boxwarden/internal/clipboardx"
	"github.com/weshofmann/boxwarden/internal/guestproto"
	"github.com/weshofmann/boxwarden/internal/session"
	"github.com/weshofmann/boxwarden/internal/sshx"
	"testing"
)

type ownerClipboardClient struct {
	*readyClient
	calls int
	run   func(context.Context, sshx.Connection, guestproto.ClipboardRequest, []byte) (guestproto.ClipboardResponse, []byte, error)
}

func (c *ownerClipboardClient) Clipboard(ctx context.Context, conn sshx.Connection, req guestproto.ClipboardRequest, data []byte) (guestproto.ClipboardResponse, []byte, error) {
	c.calls++
	return c.run(ctx, conn, req, data)
}
func readyClipboardFixture(t *testing.T) (*fixture, *ownerClipboardClient) {
	t.Helper()
	f, ready := readyFixture(t)
	for _, op := range []func(context.Context) error{func(ctx context.Context) error { return f.owner.Start(ctx, f.request) }, f.owner.Bootstrap, f.owner.Ready} {
		if err := op(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { _ = f.owner.Stop(context.Background()); _ = f.owner.Wait(context.Background()) })
	f.record.IntendedState = session.StateRunning
	f.record.Readiness = session.ReadinessRecord{Status: session.ReadinessReady}
	if err := session.SaveRecord(f.root, f.record.Domain, f.record); err != nil {
		t.Fatal(err)
	}
	client := &ownerClipboardClient{readyClient: ready}
	client.run = func(_ context.Context, _ sshx.Connection, req guestproto.ClipboardRequest, data []byte) (guestproto.ClipboardResponse, []byte, error) {
		result := []byte(nil)
		length := len(data)
		if req.Direction == "read" {
			result = []byte("雪\n\n")
			length = len(result)
		}
		return guestproto.ClipboardResponse{Version: guestproto.Version, Association: req.Association, Generation: req.Generation, Status: "ok", Length: length}, result, nil
	}
	f.owner.deps.client = client
	return f, client
}
func TestOwnerClipboardUsesExactRetainedGeneration(t *testing.T) {
	f, c := readyClipboardFixture(t)
	original := c.run
	c.run = func(ctx context.Context, conn sshx.Connection, req guestproto.ClipboardRequest, data []byte) (guestproto.ClipboardResponse, []byte, error) {
		if req.Generation != f.record.StartGeneration || req.SessionID != f.record.ID || req.Domain != "work" || req.BackendObject != f.record.Backend.ObjectID || conn != f.owner.connection {
			t.Fatal("clipboard escaped retained generation")
		}
		return original(ctx, conn, req, data)
	}
	got, err := f.owner.ReadClipboard(context.Background())
	if err != nil || !bytes.Equal(got, []byte("雪\n\n")) {
		t.Fatalf("read: %v", err)
	}
	outcome, err := f.owner.WriteClipboard(context.Background(), []byte("é\n"))
	if err != nil || outcome != clipboardx.Committed || c.calls != 2 {
		t.Fatalf("write: %s %v", outcome, err)
	}
}
func TestOwnerClipboardDurableDriftRefusesBeforeGuestCall(t *testing.T) {
	for _, kind := range []string{"generation", "state", "backend", "readiness"} {
		t.Run(kind, func(t *testing.T) {
			f, c := readyClipboardFixture(t)
			switch kind {
			case "generation":
				f.record.StartGeneration = "00000000-0000-4000-8000-000000000004"
			case "state":
				f.record.IntendedState = session.StateStopping
				f.record.Readiness.Status = session.ReadinessNotReady
			case "backend":
				f.record.Backend.ObjectID = "different-object"
			case "readiness":
				f.record.Readiness.Status = session.ReadinessDrift
			}
			if err := session.SaveRecord(f.root, f.record.Domain, f.record); err != nil {
				t.Fatal(err)
			}
			outcome, err := f.owner.WriteClipboard(context.Background(), []byte("value"))
			if outcome != clipboardx.Unchanged || !errors.Is(err, clipboardx.ErrAdmission) || c.calls != 0 {
				t.Fatalf("durable drift dispatched: %s %v", outcome, err)
			}
		})
	}
}
func TestOwnerClipboardLiveReadinessLossRefusesBeforeGuestCall(t *testing.T) {
	f, c := readyClipboardFixture(t)
	c.probe = func(sshx.Connection) error { return errors.New("synthetic failure") }
	outcome, err := f.owner.WriteClipboard(context.Background(), []byte("value"))
	if outcome != clipboardx.Unchanged || !errors.Is(err, clipboardx.ErrAdmission) || c.calls != 0 {
		t.Fatalf("unready dispatched: %s %v", outcome, err)
	}
}
func TestOwnerClipboardReadinessRaceMakesWriteUnknownAndReadUnavailable(t *testing.T) {
	for _, direction := range []string{"write", "read"} {
		t.Run(direction, func(t *testing.T) {
			f, c := readyClipboardFixture(t)
			original := c.run
			c.run = func(ctx context.Context, conn sshx.Connection, req guestproto.ClipboardRequest, data []byte) (guestproto.ClipboardResponse, []byte, error) {
				response, payload, err := original(ctx, conn, req, data)
				c.probe = func(sshx.Connection) error { return errors.New("lost READY") }
				return response, payload, err
			}
			if direction == "write" {
				outcome, err := f.owner.WriteClipboard(context.Background(), []byte("value"))
				if outcome != clipboardx.Unknown || !errors.Is(err, clipboardx.ErrUnknown) || c.calls != 1 {
					t.Fatalf("ready race write: %s %v", outcome, err)
				}
			} else {
				got, err := f.owner.ReadClipboard(context.Background())
				if len(got) != 0 || !errors.Is(err, clipboardx.ErrRead) || c.calls != 1 {
					t.Fatalf("ready race read: %v", err)
				}
			}
		})
	}
}
func TestOwnerClipboardDurableChangeAfterDispatchMakesWriteUnknown(t *testing.T) {
	f, c := readyClipboardFixture(t)
	original := c.run
	c.run = func(ctx context.Context, conn sshx.Connection, req guestproto.ClipboardRequest, data []byte) (guestproto.ClipboardResponse, []byte, error) {
		response, payload, err := original(ctx, conn, req, data)
		f.record.StartGeneration = "00000000-0000-4000-8000-000000000004"
		if err := session.SaveRecord(f.root, f.record.Domain, f.record); err != nil {
			t.Fatal(err)
		}
		return response, payload, err
	}
	outcome, err := f.owner.WriteClipboard(context.Background(), []byte("value"))
	if outcome != clipboardx.Unknown || !errors.Is(err, clipboardx.ErrUnknown) || c.calls != 1 {
		t.Fatalf("record race: %s %v", outcome, err)
	}
}
func TestOwnerClipboardWriteOutcomeClassification(t *testing.T) {
	for _, kind := range []string{"error", "unknown", "transport", "pre-dispatch", "badbinding", "badlength"} {
		t.Run(kind, func(t *testing.T) {
			f, c := readyClipboardFixture(t)
			original := c.run
			c.run = func(ctx context.Context, conn sshx.Connection, req guestproto.ClipboardRequest, data []byte) (guestproto.ClipboardResponse, []byte, error) {
				response, _, _ := original(ctx, conn, req, data)
				switch kind {
				case "error":
					response.Status = "error"
					response.Length = 0
				case "unknown":
					response.Status = "unknown"
					response.Length = 0
				case "transport":
					return guestproto.ClipboardResponse{}, nil, errors.New("synthetic private payload")
				case "pre-dispatch":
					return guestproto.ClipboardResponse{}, nil, clipboardx.ErrAdmission
				case "badbinding":
					response.SessionID = "00000000-0000-4000-8000-000000000004"
				case "badlength":
					response.Length++
				}
				return response, nil, nil
			}
			outcome, err := f.owner.WriteClipboard(context.Background(), []byte("value"))
			if kind == "error" || kind == "pre-dispatch" {
				if outcome != clipboardx.Unchanged || err == nil {
					t.Fatalf("refusal: %s %v", outcome, err)
				}
			} else if outcome != clipboardx.Unknown || err != clipboardx.ErrUnknown {
				t.Fatalf("uncertain: %s %v", outcome, err)
			}
		})
	}
}
func TestOwnerClipboardInvalidTextAndCancelledBeforeDispatch(t *testing.T) {
	f, c := readyClipboardFixture(t)
	outcome, err := f.owner.WriteClipboard(context.Background(), []byte{0xff})
	if outcome != clipboardx.Unchanged || err != clipboardx.ErrInvalidText || c.calls != 0 {
		t.Fatalf("invalid: %s %v", outcome, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	outcome, err = f.owner.WriteClipboard(ctx, []byte("value"))
	if outcome != clipboardx.Unchanged || err != clipboardx.ErrCancelled || c.calls != 0 {
		t.Fatalf("cancel: %s %v", outcome, err)
	}
}
