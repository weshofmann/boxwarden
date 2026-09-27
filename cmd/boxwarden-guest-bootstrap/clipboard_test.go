package main

import (
	"bytes"
	"context"
	"github.com/weshofmann/boxwarden/internal/guestproto"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type desktopFake struct{ calls int }

func (f *desktopFake) Run(_ context.Context, _ string, payload []byte) ([]byte, error) {
	f.calls++
	return []byte(`{"version":1,"status":"ok","length":2}` + "\n"), nil
}
func TestClipboardModeFixedBoundRequestAndReceipt(t *testing.T) {
	b, input := serialFixture(t)
	if err := run([]string{"serial-bootstrap"}, input, &bytes.Buffer{}, nil, b); err != nil {
		t.Fatal(err)
	}
	fake := &desktopFake{}
	b.ClipboardExecutor = fake
	request := guestproto.ClipboardRequest{Version: 1, Association: guestproto.Association{Domain: "work", SessionID: "123e4567-e89b-42d3-a456-426614174000", BackendKind: "tart", BackendObject: "workstation"}, Generation: "9b2d12d8-7014-4c5e-9d5c-627c2fcc1575", ExpiresAt: time.Now().UTC().Add(20 * time.Second), Direction: "write"}
	// serialFixture currently uses the same fixed test generation.
	record := filepath.Join(b.Root, "run/boxwarden/clipboard-generation.json")
	raw, err := os.ReadFile(record)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(raw, []byte(request.Generation)) {
		t.Fatalf("fixture generation differs: %s", raw)
	}
	frame, err := guestproto.EncodeClipboardRequest(request, []byte("ok"))
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := run([]string{"clipboard"}, bytes.NewReader(frame), &out, nil, b); err != nil {
		t.Fatal(err)
	}
	response, payload, err := guestproto.DecodeClipboardResponse(request, bytes.NewReader(out.Bytes()))
	if err != nil || response.Status != "ok" || response.Length != 2 || len(payload) != 0 || fake.calls != 1 {
		t.Fatalf("bad receipt: %+v %v", response, err)
	}
	if err := run([]string{"clipboard"}, bytes.NewReader(append(frame, 1)), &bytes.Buffer{}, nil, b); err == nil || fake.calls != 1 {
		t.Fatal("trailing request invoked adapter")
	}
}
