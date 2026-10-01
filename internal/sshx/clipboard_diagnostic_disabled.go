//go:build !n1clipboarddiagnostic || n1candidate

package sshx

import (
	"context"
	"github.com/weshofmann/boxwarden/internal/guestproto"
)

func clipboardStage(context.Context, string, string) {}
func clipboardDiagnosticEnvelope(_ context.Context, _ guestproto.ClipboardRequest, encoded []byte) ([]byte, error) {
	return encoded, nil
}
func clipboardHelperArguments(_ context.Context, c Connection) []string {
	return fixedGuestHelperArguments(c, "clipboard")
}

func clipboardDiagnosticReady(context.Context) bool { return true }

func configureClipboardDiagnosticClient(*Client)                  {}
func clipboardExchangeRunner(_ context.Context, c *Client) Runner { return c.clipboardRunner }
func clipboardObservePublication(context.Context, Result, error)  {}

func clipboardTransferTruncated(_ context.Context, r Result) bool { return r.Truncated }
