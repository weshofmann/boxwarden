//go:build n1clipboarddiagnostic && !n1candidate

package sshx

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/weshofmann/boxwarden/internal/clipboarddiag"
	"github.com/weshofmann/boxwarden/internal/execx"
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

func TestDiagnosticCollectionDrainChild(t *testing.T) {
	if len(os.Args) != 4 || os.Args[2] != "--" {
		return
	}
	mode := os.Args[3]
	raw, _ := io.ReadAll(os.Stdin)
	op, e := clipboarddiag.DecodeOperation(raw, false)
	if e != nil {
		os.Exit(41)
	}
	receipt, _ := json.Marshal(clipboarddiag.GuestCollection{Version: 1, Binding: op})
	if mode == "stdout" || mode == "both" {
		fmt.Fprint(os.Stdout, strings.Repeat("m", 32769))
	} else {
		os.Stdout.Write(append(receipt, '\n'))
	}
	if mode == "stderr" || mode == "both" {
		fmt.Fprint(os.Stderr, strings.Repeat("m", 4097))
	}
	if mode == "valid" || mode == "stdout" {
		digest := clipboarddiag.MetadataDigest([]byte(clipboarddiag.HeaderDigest(op) + "\n"))
		w, _ := clipboarddiag.EncodePublication(clipboarddiag.PublicationWitness{V: 1, Phase: "collect", Header: clipboarddiag.HeaderDigest(op), Generation: clipboarddiag.GenerationDigest(op), Collect: &digest})
		os.Stderr.Write(w)
	}
	os.Exit(0)
}

type diagnosticCollectionDrainFixture struct {
	runner   execx.OSRunner
	mode     string
	calls    int
	observed execx.Result
}

func (f *diagnosticCollectionDrainFixture) Run(ctx context.Context, c execx.Command) (execx.Result, error) {
	f.calls++
	c.Env = append(c.Env, "GORACE=atexit_sleep_ms=0")
	c.Path = os.Args[0]
	c.Args = []string{"-test.run=^TestDiagnosticCollectionDrainChild$", "--", f.mode}
	result, e := f.runner.Run(ctx, c)
	f.observed = result
	return result, e
}
func TestDiagnosticCollectionActualDrainQuotasAndOneCall(t *testing.T) {
	for _, mode := range []string{"valid", "stdout", "stderr", "both"} {
		t.Run(mode, func(t *testing.T) {
			conn := testConnection(t)
			conn.Binding.Domain = "n1qualification"
			conn.Pin.Domain = "n1qualification"
			req := clipboardRequest(conn, "read")
			op := clipboarddiag.Operation{Version: 1, OperationID: "00000000-0000-4000-8000-000000000001", Direction: "read", Domain: req.Domain, SessionID: req.SessionID, BackendKind: req.BackendKind, BackendObject: req.BackendObject, Generation: req.Generation, ExpiresAt: time.Now().UTC().Add(time.Second)}
			production := newClipboardDiagnosticCollectionRunner().runner.(execx.OSRunner)
			if production.MaxStdoutBytes != 32768 || production.MaxStderrBytes != 4096 || production.MaxStdinBytes != 4096 {
				t.Fatal("production factory lacks fixed collection caps")
			}
			fixture := &diagnosticCollectionDrainFixture{runner: production, mode: mode}
			client := NewClientWithClipboardRunner(nil, newExecRunner(fixture))
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			_, err := client.collectClipboardDiagnosticWith(ctx, conn, op, newExecRunner(fixture))
			if fixture.calls != 1 {
				t.Fatal("collection did not invoke exactly once")
			}
			if len(fixture.observed.Stdout) > 32768 || len(fixture.observed.Stderr) > 4096 {
				t.Fatal("actual drain retained ordinary payload quota", len(fixture.observed.Stdout), len(fixture.observed.Stderr))
			}
			if (err == nil) != (mode == "valid") || fixture.observed.Truncated != (mode != "valid") {
				t.Fatal("overrun decoder/truncation admission changed", err)
			}
		})
	}
}
