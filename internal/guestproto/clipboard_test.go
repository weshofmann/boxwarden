package guestproto

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func clipboardRequest() ClipboardRequest {
	return ClipboardRequest{Version: Version, Association: testRequest().Association, Generation: testGeneration, ExpiresAt: time.Now().UTC().Add(20 * time.Second), Direction: "write"}
}

func TestClipboardRequestExactRoundTrip(t *testing.T) {
	req := clipboardRequest()
	for _, payload := range [][]byte{{}, []byte("雪\n\r\n\t "), bytes.Repeat([]byte("x"), 1<<20)} {
		frame, err := EncodeClipboardRequest(req, payload)
		if err != nil {
			t.Fatal(err)
		}
		got, raw, err := DecodeClipboardRequest(bytes.NewReader(frame))
		if err != nil || got != req || !bytes.Equal(raw, payload) {
			t.Fatalf("roundtrip failed: %v", err)
		}
	}
}
func TestClipboardRequestRejectsInvalidAndTrailing(t *testing.T) {
	req := clipboardRequest()
	valid, _ := EncodeClipboardRequest(req, []byte("ok"))
	split := bytes.IndexByte(valid, '\n')
	oversize := append([]byte{}, valid[:split+1]...)
	length := make([]byte, 4)
	binary.BigEndian.PutUint32(length, 1048577)
	oversize = append(oversize, length...)
	malformed := [][]byte{append(append([]byte{}, valid...), 1), valid[:len(valid)-1], oversize, bytes.Replace(valid, []byte(`"version":1`), []byte(`"version":1,"version":1`), 1), bytes.Replace(valid, []byte(`"version":1`), []byte(`"version": 1`), 1)}
	for _, raw := range malformed {
		if _, _, err := DecodeClipboardRequest(bytes.NewReader(raw)); err == nil {
			t.Fatal("malformed request accepted")
		}
	}
	for _, raw := range [][]byte{[]byte("secret\x00"), []byte("secret\xff"), bytes.Repeat([]byte("x"), 1048577)} {
		if _, err := EncodeClipboardRequest(req, raw); err == nil || strings.Contains(err.Error(), "secret") {
			t.Fatal("invalid or disclosed text")
		}
	}
	req.Direction = "read"
	if _, err := EncodeClipboardRequest(req, []byte("unexpected")); err == nil {
		t.Fatal("read accepted payload")
	}
}
func TestClipboardResponseBindingAndExactEmpty(t *testing.T) {
	req := clipboardRequest()
	req.Direction = "read"
	for _, payload := range [][]byte{{}, []byte("雪\n\n")} {
		resp := ClipboardResponse{Version: Version, Association: req.Association, Generation: req.Generation, Status: "ok", Length: len(payload)}
		raw, err := EncodeClipboardResponse(req, resp, payload)
		if err != nil {
			t.Fatal(err)
		}
		got, data, err := DecodeClipboardResponse(req, bytes.NewReader(raw))
		if err != nil || got != resp || !bytes.Equal(data, payload) {
			t.Fatalf("bad response: %v", err)
		}
		foreign := req
		foreign.Generation = "80c64529-fcb5-4789-8460-a43517622238"
		if _, _, err := DecodeClipboardResponse(foreign, bytes.NewReader(raw)); err == nil {
			t.Fatal("stale generation accepted")
		}
		foreign = req
		foreign.Domain = "personal"
		if _, _, err := DecodeClipboardResponse(foreign, bytes.NewReader(raw)); err == nil {
			t.Fatal("foreign association accepted")
		}
		if _, _, err := DecodeClipboardResponse(req, bytes.NewReader(append(raw, 'x'))); err == nil {
			t.Fatal("trailing response accepted")
		}
	}
}

type clipboardFake struct {
	calls int
	fn    func() ([]byte, error)
}

func (f *clipboardFake) Run(_ context.Context, direction string, payload []byte) ([]byte, error) {
	f.calls++
	return f.fn()
}
func clipboardFixture(t *testing.T) (*Bootstrapper, ClipboardRequest, *clipboardFake) {
	t.Helper()
	b, _ := testBootstrapper(t)
	if _, err := b.Serial(context.Background(), testRequest()); err != nil {
		t.Fatal(err)
	}
	fake := &clipboardFake{fn: func() ([]byte, error) { return []byte(`{"version":1,"status":"ok","length":2}` + "\n"), nil }}
	b.ClipboardExecutor = fake
	return b, clipboardRequest(), fake
}
func TestClipboardAdmissionRejectsStaleSpoofedMissingAndAmbiguousGeneration(t *testing.T) {
	for _, kind := range []string{"stale", "spoofed", "missing", "duplicate", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			b, req, fake := clipboardFixture(t)
			record := filepath.Join(b.Root, "run/boxwarden/clipboard-generation.json")
			switch kind {
			case "stale":
				req.Generation = "80c64529-fcb5-4789-8460-a43517622238"
			case "spoofed":
				req.Domain = "personal"
			case "missing":
				if err := os.Remove(record); err != nil {
					t.Fatal(err)
				}
			case "duplicate":
				raw, err := os.ReadFile(record)
				if err != nil {
					t.Fatal(err)
				}
				raw = bytes.Replace(raw, []byte(`"version":1`), []byte(`"version":1,"version":1`), 1)
				if err := os.WriteFile(record, raw, 0600); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				if err := os.Remove(record); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink("/tmp/unrelated", record); err != nil {
					t.Fatal(err)
				}
			}
			if _, _, err := b.Clipboard(context.Background(), req, []byte("ok")); err == nil {
				t.Fatal("bad admission accepted")
			}
			if fake.calls != 0 {
				t.Fatal("adapter ran before admission")
			}
		})
	}
}
func TestClipboardSerialRefreshesEphemeralGenerationWithoutChangingTrust(t *testing.T) {
	b, req, _ := clipboardFixture(t)
	manifest := filepath.Join(b.Root, "etc/ssh/boxwarden/active/management-binding.json")
	before, _ := os.ReadFile(manifest)
	serial := testRequest()
	serial.StartGeneration = "80c64529-fcb5-4789-8460-a43517622238"
	if _, err := b.Serial(context.Background(), serial); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(manifest)
	if !bytes.Equal(before, after) {
		t.Fatal("trust manifest changed")
	}
	if _, _, err := b.Clipboard(context.Background(), req, []byte("ok")); err == nil {
		t.Fatal("old generation admitted")
	}
	req.Generation = serial.StartGeneration
	resp, _, err := b.Clipboard(context.Background(), req, []byte("ok"))
	if err != nil || resp.Status != "ok" {
		t.Fatalf("new generation rejected: %v", err)
	}
}
func TestClipboardWriteDispatchFailureOrBadAckIsUnknown(t *testing.T) {
	for _, output := range []string{"", `{"version":1,"status":"ok","length":3}` + "\n", `{"version":1,"status":"ok","length":2,"extra":"secret"}` + "\n", `{"version":1,"status":"unknown","length":0}` + "\n"} {
		b, req, fake := clipboardFixture(t)
		fake.fn = func() ([]byte, error) { return []byte(output), errors.New("secret backend detail") }
		resp, raw, err := b.Clipboard(context.Background(), req, []byte("ok"))
		if err != nil || resp.Status != "unknown" || len(raw) != 0 {
			t.Fatalf("wrong possible-commit result: %v", err)
		}
	}
}
func TestClipboardExplicitErrorPreservesPrecommitAndReadExact(t *testing.T) {
	b, req, fake := clipboardFixture(t)
	fake.fn = func() ([]byte, error) {
		return []byte(`{"version":1,"status":"error","length":0}` + "\n"), errors.New("secret")
	}
	resp, _, err := b.Clipboard(context.Background(), req, []byte("ok"))
	if err != nil || resp.Status != "error" {
		t.Fatalf("explicit refusal not preserved: %v", err)
	}
	req.Direction = "read"
	fake.fn = func() ([]byte, error) { return []byte(`{"version":1,"status":"ok","length":5}` + "\n雪\n\n"), nil }
	resp, raw, err := b.Clipboard(context.Background(), req, nil)
	if err != nil || resp.Status != "ok" || !bytes.Equal(raw, []byte("雪\n\n")) {
		t.Fatalf("read failed: %v", err)
	}
}
func TestClipboardPostdispatchGenerationReplacementIsUnknownForWrite(t *testing.T) {
	b, req, fake := clipboardFixture(t)
	fake.fn = func() ([]byte, error) {
		_ = os.Remove(filepath.Join(b.Root, "run/boxwarden/clipboard-generation.json"))
		return []byte(`{"version":1,"status":"ok","length":2}` + "\n"), nil
	}
	resp, _, err := b.Clipboard(context.Background(), req, []byte("ok"))
	if err != nil || resp.Status != "unknown" {
		t.Fatalf("postcommit stale generation result: %v", err)
	}
}

func TestClipboardExpiryAdmissionBeforeExecutor(t *testing.T) {
	for _, expiry := range []time.Time{time.Time{}, time.Now().UTC().Add(-time.Second), time.Now().UTC().Add(31 * time.Second)} {
		b, req, fake := clipboardFixture(t)
		req.ExpiresAt = expiry
		if _, _, err := b.Clipboard(context.Background(), req, []byte("ok")); err == nil {
			t.Fatal("invalid expiry admitted")
		}
		if fake.calls != 0 {
			t.Fatal("expired request reached mutation")
		}
	}
}
func TestClipboardWireRequiresExactExpiry(t *testing.T) {
	req := clipboardRequest()
	raw, _ := EncodeClipboardRequest(req, []byte("ok"))
	field := []byte(`,"expires_at":"` + req.ExpiresAt.Format(time.RFC3339Nano) + `"`)
	for _, frame := range [][]byte{bytes.Replace(raw, field, nil, 1), bytes.Replace(raw, field, append(append([]byte{}, field...), field...), 1), bytes.Replace(raw, field, []byte(`,"expires_at":"1970-01-01T00:00:00Z"`), 1)} {
		if _, _, err := DecodeClipboardRequest(bytes.NewReader(frame)); err == nil {
			t.Fatal("bad expiry wire accepted")
		}
	}
}
func TestClipboardExecutorInheritsRequestDeadline(t *testing.T) {
	b, req, fake := clipboardFixture(t)
	fake.fn = func() ([]byte, error) { return []byte(`{"version":1,"status":"ok","length":2}` + "\n"), nil }
	b.ClipboardExecutor = &deadlineClipboardFake{want: req.ExpiresAt, t: t}
	if _, _, err := b.Clipboard(context.Background(), req, []byte("ok")); err != nil {
		t.Fatal(err)
	}
}

type deadlineClipboardFake struct {
	want time.Time
	t    *testing.T
}

func (f *deadlineClipboardFake) Run(ctx context.Context, _ string, _ []byte) ([]byte, error) {
	got, ok := ctx.Deadline()
	if !ok || !got.Equal(f.want) {
		f.t.Fatal("executor received a fresh rather than shared deadline")
	}
	return []byte(`{"version":1,"status":"ok","length":2}` + "\n"), nil
}
