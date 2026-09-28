package main

import (
	"bytes"
	"context"
	"errors"
	"github.com/weshofmann/boxwarden/internal/clipboardx"
	"testing"
)

func TestInternalPasteboardModeRequiresExactFixedArguments(t *testing.T) {
	var output bytes.Buffer
	handled, err := runInternal(context.Background(), []string{"internal", "clipboard-pasteboard", "unknown", "org.boxwarden.test.dispatch"}, bytes.NewReader(nil), &output, nil)
	if !handled || !errors.Is(err, clipboardx.ErrRequest) || output.Len() != 0 {
		t.Fatalf("invalid private mode: handled=%v err=%v", handled, err)
	}
	handled, err = runInternal(context.Background(), []string{"internal", "clipboard-pasteboard", "read", "org.boxwarden.test.dispatch", "extra"}, bytes.NewReader(nil), &output, nil)
	if !handled || err == nil || output.Len() != 0 {
		t.Fatalf("extra args accepted: handled=%v err=%v", handled, err)
	}
}
