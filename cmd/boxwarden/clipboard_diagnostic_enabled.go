//go:build n1clipboarddiagnostic && !n1candidate

package main

import (
	"context"
	"github.com/weshofmann/boxwarden/internal/clipboarddiag"
	"github.com/weshofmann/boxwarden/internal/clipboardx"
	"io"
	"time"
)

func runClipboardDiagnosticInternal(ctx context.Context, args []string, input io.Reader, output io.Writer) (bool, error) {
	if len(args) < 2 || args[1] != "n1-clipboard-diagnostic" {
		return false, nil
	}
	if len(args) != 3 || (args[2] != "invoke" && args[2] != "collect") {
		return true, clipboardx.ErrRequest
	}
	limit := clipboarddiag.MaxHeaderBytes
	if args[2] == "collect" {
		limit = clipboarddiag.MaxFragmentBytes
	}
	intakeCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	type intake struct {
		raw []byte
		err error
	}
	ready := make(chan intake, 1)
	go func() { raw, err := io.ReadAll(io.LimitReader(input, int64(limit)+1)); ready <- intake{raw, err} }()
	var raw []byte
	select {
	case message := <-ready:
		if message.err != nil {
			return true, clipboardx.ErrRequest
		}
		raw = message.raw
	case <-intakeCtx.Done():
		return true, clipboardx.ErrRequest
	}
	return true, runDiagnosticCommand(ctx, args[2], raw, output)
}
