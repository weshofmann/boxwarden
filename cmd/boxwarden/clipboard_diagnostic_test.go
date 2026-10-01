//go:build n1clipboarddiagnostic && !n1candidate

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/weshofmann/boxwarden/internal/clipboarddiag"
	"github.com/weshofmann/boxwarden/internal/clipboardx"
	"github.com/weshofmann/boxwarden/internal/config"
	"github.com/weshofmann/boxwarden/internal/session"
	"io"
	"strings"
	"testing"
	"time"
)

func TestN1ClipboardDiagnosticInternalRejectsInvalidBinding(t *testing.T) {
	handled, err := runInternal(context.Background(), []string{"internal", "n1-clipboard-diagnostic", "invoke"}, strings.NewReader("{}"), io.Discard, nil)
	if !handled || !errors.Is(err, clipboardx.ErrRequest) {
		t.Fatalf("fixed diagnostic binding admission missing: handled=%v error=%v", handled, err)
	}
}
func TestDiagnosticCLIFixedConfigRoleAndStrictBindingBeforeLoader(t *testing.T) {
	op := clipboarddiag.Operation{Version: 1, OperationID: "00000000-0000-4000-8000-000000000001", Direction: "write", Domain: "n1qualification", SessionID: "00000000-0000-4000-8000-000000000002", BackendKind: "tart", BackendObject: "synthetic", Generation: "00000000-0000-4000-8000-000000000003", ExpiresAt: time.Now().UTC().Add(time.Second)}
	raw, _ := json.Marshal(diagnosticCommandRequest{1, "synthetic", op})
	calls := 0
	loader := func(path string) (config.Config, error) {
		calls++
		if path != clipboardDiagnosticConfigPath || !strings.HasSuffix(path, ".enrolled.json") {
			t.Fatal("ambient config selected")
		}
		return config.Config{}, errors.New("synthetic refusal")
	}
	factory := func(context.Context, config.Config, config.Domain) (*session.ClipboardDiagnosticService, error) {
		panic("missing fixed config reached factory")
	}
	for _, bad := range [][]byte{[]byte("{}"), append(append([]byte{}, raw...), raw...), bytes.Replace(raw, []byte(`"version":1`), []byte(`"version":1,"version":1`), 1), bytes.Replace(raw, []byte(`"version":1`), []byte(`"version":true`), 1), append(append([]byte{}, raw[:len(raw)-1]...), []byte(`,"config":"/arbitrary"}`)...)} {
		if err := runDiagnosticCommandWith(t.Context(), "invoke", bad, io.Discard, loader, factory); !errors.Is(err, clipboardx.ErrRequest) {
			t.Fatal("bad command binding accepted", err)
		}
	}
	if calls != 0 {
		t.Fatal("invalid binding reached config admission")
	}
	if err := runDiagnosticCommandWith(t.Context(), "invoke", raw, io.Discard, loader, factory); !errors.Is(err, clipboardx.ErrAdmission) || calls != 1 {
		t.Fatal("fixed config refusal missing", err)
	}
}

func TestDiagnosticCLIStalledIntakeHonorsContext(t *testing.T) {
	reader, writer := io.Pipe()
	defer reader.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := runClipboardDiagnosticInternal(ctx, []string{"internal", "n1-clipboard-diagnostic", "invoke"}, reader, io.Discard)
		done <- err
	}()
	select {
	case <-done:
		writer.Close()
	case <-time.After(100 * time.Millisecond):
		writer.Close()
		<-done
		t.Fatal("stalled CLI intake exceeded context")
	}
}
