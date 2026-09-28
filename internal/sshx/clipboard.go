package sshx

import (
	"bytes"
	"context"

	"github.com/weshofmann/boxwarden/internal/clipboardx"
	"github.com/weshofmann/boxwarden/internal/execx"
	"github.com/weshofmann/boxwarden/internal/guestproto"
)

// NewClipboardExecRunner provides only the fixed clipboard exchange's larger
// stdin/stdout budget. Generic management runners retain their existing bounds.
func NewClipboardExecRunner() ExecRunner {
	return newExecRunner(execx.OSRunner{MaxStdinBytes: guestproto.MaxClipboardRequestBytes, MaxOutputBytes: guestproto.MaxClipboardResponseBytes})
}

// NewClientWithClipboardRunner injects independently bounded transports.
func NewClientWithClipboardRunner(management, clipboard Runner) *Client {
	return &Client{runner: management, clipboardRunner: clipboard}
}

// Clipboard invokes one fixed helper mode through the exact pinned connection.
// Payloads are stdin bytes only. After write dispatch, transport/protocol failure
// is unknown because the guest may already own the destination selection.
func (c *Client) Clipboard(ctx context.Context, connection Connection, request guestproto.ClipboardRequest, payload []byte) (guestproto.ClipboardResponse, []byte, error) {
	if c == nil || c.clipboardRunner == nil {
		return guestproto.ClipboardResponse{}, nil, clipboardx.ErrAdmission
	}
	if validateConnection(connection) != nil {
		return guestproto.ClipboardResponse{}, nil, clipboardx.ErrAdmission
	}
	if request.Association != (guestproto.Association{Domain: string(connection.Binding.Domain), SessionID: connection.Binding.SessionID, BackendKind: connection.Binding.BackendKind, BackendObject: connection.Binding.BackendObject}) {
		return guestproto.ClipboardResponse{}, nil, clipboardx.ErrAdmission
	}
	ctx, cancel := context.WithTimeout(ctx, clipboardx.TransferTimeout)
	defer cancel()
	if ctx.Err() != nil {
		return guestproto.ClipboardResponse{}, nil, clipboardx.ErrCancelled
	}
	deadline, _ := ctx.Deadline()
	// Preserve an earlier captured expiry; never lend a queued request a fresh window.
	if request.ExpiresAt.IsZero() || deadline.Before(request.ExpiresAt) {
		request.ExpiresAt = deadline.UTC()
	}
	encoded, err := guestproto.EncodeClipboardRequest(request, payload)
	if err != nil || len(encoded) > guestproto.MaxClipboardRequestBytes {
		return guestproto.ClipboardResponse{}, nil, clipboardx.ErrRequest
	}
	if ctx.Err() != nil {
		return guestproto.ClipboardResponse{}, nil, clipboardx.ErrCancelled
	}
	if verifyKnownHostsPin(connection) != nil {
		return guestproto.ClipboardResponse{}, nil, clipboardx.ErrAdmission
	}
	if ctx.Err() != nil {
		return guestproto.ClipboardResponse{}, nil, clipboardx.ErrCancelled
	}
	result, runErr := c.clipboardRunner.Run(ctx, Command{Path: sshPath, Args: fixedGuestHelperArguments(connection, "clipboard"), Stdin: encoded})
	failure := clipboardx.ErrRead
	if request.Direction == "write" {
		failure = clipboardx.ErrUnknown
	}
	if runErr != nil || ctx.Err() != nil || result.Truncated || len(result.Stdout) > guestproto.MaxClipboardResponseBytes || len(result.Stderr) > guestproto.MaxClipboardMetadataBytes {
		return guestproto.ClipboardResponse{}, nil, failure
	}
	response, data, err := guestproto.DecodeClipboardResponse(request, bytes.NewReader([]byte(result.Stdout)))
	if err != nil || (request.Direction == "write" && response.Status == "ok" && response.Length != len(payload)) {
		return guestproto.ClipboardResponse{}, nil, failure
	}
	return response, data, nil
}
