package session

import (
	"bytes"
	"context"
	"errors"
	"github.com/weshofmann/boxwarden/internal/clipboardx"
	"github.com/weshofmann/boxwarden/internal/config"
	"github.com/weshofmann/boxwarden/internal/lock"
	"testing"
)

type clipboardEndpointFake struct {
	begin func(context.Context, clipboardx.Target, clipboardx.Direction) (clipboardx.Transfer, error)
}

func (f clipboardEndpointFake) Begin(ctx context.Context, target clipboardx.Target, d clipboardx.Direction) (clipboardx.Transfer, error) {
	return f.begin(ctx, target, d)
}

type clipboardTransferFake struct {
	closed bool
	read   []byte
	write  []byte
	close  func()
}

func (f *clipboardTransferFake) Write(_ context.Context, text []byte) (clipboardx.Outcome, error) {
	f.write = append([]byte{}, text...)
	return clipboardx.Committed, nil
}
func (f *clipboardTransferFake) Read(context.Context) ([]byte, error) { return f.read, nil }
func (f *clipboardTransferFake) Close() error {
	f.closed = true
	if f.close != nil {
		f.close()
	}
	return nil
}

type clipboardBoardFake struct {
	reads int
	check func()
}

func (f *clipboardBoardFake) ReadText(context.Context) ([]byte, error) {
	f.reads++
	if f.check != nil {
		f.check()
	}
	return []byte("synthetic ☃\n"), nil
}
func (f *clipboardBoardFake) WriteText(context.Context, []byte) (clipboardx.Outcome, error) {
	return clipboardx.Committed, nil
}
func TestClipboardSessionLocksSpanCaptureAndTransferClose(t *testing.T) {
	root, a := actionAttemptFixture(t)
	board := &clipboardBoardFake{}
	transfer := &clipboardTransferFake{}
	requireLocked := func() {
		for _, scope := range []string{"transition-work-dev", "session-work-dev"} {
			h, err := lock.TryAcquire(t.Context(), root, scope)
			if h != nil {
				h.Release()
			}
			if !errors.Is(err, lock.ErrBusy) {
				t.Fatalf("lock not retained %s: %v", scope, err)
			}
		}
	}
	board.check = requireLocked
	transfer.close = requireLocked
	endpoint := clipboardEndpointFake{begin: func(_ context.Context, target clipboardx.Target, d clipboardx.Direction) (clipboardx.Transfer, error) {
		requireLocked()
		if board.reads != 0 || target.SessionID != a.SessionID || target.Generation != a.Generation || target.BackendObject != a.BackendObject || target.Domain != "work" || target.BackendKind != "tart" || d != clipboardx.ToGuest {
			t.Fatal("source before exact admission")
		}
		return transfer, nil
	}}
	service := NewClipboardService(config.Domain{ID: "work", StateRoot: root}, endpoint)
	out, err := service.Execute(t.Context(), "dev", clipboardx.Request{Mode: clipboardx.Push}, nil, nil, board)
	if err != nil || out != clipboardx.Committed || !transfer.closed || !bytes.Equal(transfer.write, []byte("synthetic ☃\n")) {
		t.Fatalf("execute %v %v", out, err)
	}
	for _, scope := range []string{"transition-work-dev", "session-work-dev"} {
		h, err := lock.TryAcquire(t.Context(), root, scope)
		if err != nil {
			t.Fatal(err)
		}
		h.Release()
	}
}
func TestClipboardSessionBusyAndStaleNeverCapture(t *testing.T) {
	root, _ := actionAttemptFixture(t)
	board := &clipboardBoardFake{}
	calls := 0
	service := NewClipboardService(config.Domain{ID: "work", StateRoot: root}, clipboardEndpointFake{begin: func(context.Context, clipboardx.Target, clipboardx.Direction) (clipboardx.Transfer, error) {
		calls++
		return &clipboardTransferFake{}, nil
	}})
	for _, scope := range []string{"transition-work-dev", "session-work-dev"} {
		h, err := lock.TryAcquire(t.Context(), root, scope)
		if err != nil {
			t.Fatal(err)
		}
		_, err = service.Execute(t.Context(), "dev", clipboardx.Request{Mode: clipboardx.Push}, nil, nil, board)
		h.Release()
		if err != clipboardx.ErrAdmission || board.reads != 0 || calls != 0 {
			t.Fatal("busy target captured")
		}
	}
	_, err := service.Execute(t.Context(), "dev", clipboardx.Request{Mode: clipboardx.Push, Target: clipboardx.Target{Generation: "different"}}, nil, nil, board)
	if err != clipboardx.ErrAdmission || board.reads != 0 || calls != 0 {
		t.Fatal("stale target captured")
	}
}
