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
	refuse := func(stage string, err error) (guestproto.ClipboardResponse, []byte, error) {
		clipboardStage(ctx, stage, "refused")
		return guestproto.ClipboardResponse{}, nil, err
	}
	if c == nil {
		return refuse("ssh_client", clipboardx.ErrAdmission)
	}
	if c.clipboardRunner == nil {
		return refuse("ssh_runner", clipboardx.ErrAdmission)
	}
	if validateConnection(connection) != nil {
		return refuse("ssh_connection", clipboardx.ErrAdmission)
	}
	if request.Association != (guestproto.Association{Domain: string(connection.Binding.Domain), SessionID: connection.Binding.SessionID, BackendKind: connection.Binding.BackendKind, BackendObject: connection.Binding.BackendObject}) {
		return refuse("ssh_association", clipboardx.ErrAdmission)
	}
	ctx, cancel := context.WithTimeout(ctx, clipboardx.TransferTimeout)
	defer cancel()
	if ctx.Err() != nil {
		return refuse("ssh_context", clipboardx.ErrCancelled)
	}
	deadline, _ := ctx.Deadline()
	if request.ExpiresAt.IsZero() || deadline.Before(request.ExpiresAt) {
		request.ExpiresAt = deadline.UTC()
	}
	encoded, err := guestproto.EncodeClipboardRequest(request, payload)
	if err != nil {
		return refuse("ssh_encoding", clipboardx.ErrRequest)
	}
	if len(encoded) > guestproto.MaxClipboardRequestBytes {
		return refuse("ssh_encoding_size", clipboardx.ErrRequest)
	}
	if ctx.Err() != nil {
		return refuse("ssh_pre_pin_context", clipboardx.ErrCancelled)
	}
	if verifyKnownHostsPin(connection) != nil {
		return refuse("ssh_pin", clipboardx.ErrAdmission)
	}
	if ctx.Err() != nil {
		return refuse("ssh_pre_dispatch_context", clipboardx.ErrCancelled)
	}
	encoded, err = clipboardDiagnosticEnvelope(ctx, request, encoded)
	if err != nil {
		return refuse("ssh_association", clipboardx.ErrAdmission)
	}
	clipboardStage(ctx, "ssh_dispatch", "ok")
	if !clipboardDiagnosticReady(ctx) {
		return refuse("ssh_diagnostic", clipboardx.ErrAdmission)
	}
	result, runErr := clipboardExchangeRunner(ctx, c).Run(ctx, Command{Path: sshPath, Args: clipboardHelperArguments(ctx, connection), Stdin: encoded})
	clipboardObservePublication(ctx, result, runErr)
	failure := clipboardx.ErrRead
	status := "unavailable"
	if request.Direction == "write" {
		failure = clipboardx.ErrUnknown
		status = "unknown"
	}
	failed := func(stage string) (guestproto.ClipboardResponse, []byte, error) {
		clipboardStage(ctx, stage, status)
		return guestproto.ClipboardResponse{}, nil, failure
	}
	if runErr != nil {
		return failed("ssh_transport")
	}
	if ctx.Err() != nil {
		return failed("ssh_post_context")
	}
	if clipboardTransferTruncated(ctx, result) {
		return failed("ssh_truncated")
	}
	if len(result.Stdout) > guestproto.MaxClipboardResponseBytes {
		return failed("ssh_stdout_bound")
	}
	if len(result.Stderr) > guestproto.MaxClipboardMetadataBytes {
		return failed("ssh_stderr_bound")
	}
	response, data, err := guestproto.DecodeClipboardResponse(request, bytes.NewReader([]byte(result.Stdout)))
	if err != nil {
		if len(result.Stdout) == 0 {
			return failed("ssh_ack_missing")
		}
		return failed("ssh_ack_malformed")
	}
	if request.Direction == "write" && response.Status == "ok" && response.Length != len(payload) {
		return failed("ssh_ack_length")
	}
	if response.Status == "unknown" {
		clipboardStage(ctx, "ssh_ack_status", "unknown")
	} else if response.Status == "error" {
		clipboardStage(ctx, "ssh_ack_status", "refused")
	} else {
		clipboardStage(ctx, "ssh_complete", "ok")
	}
	return response, data, nil
}
