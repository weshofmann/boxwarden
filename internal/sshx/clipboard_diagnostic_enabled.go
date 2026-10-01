//go:build n1clipboarddiagnostic && !n1candidate

package sshx

import (
	"context"
	"github.com/weshofmann/boxwarden/internal/clipboarddiag"
	"github.com/weshofmann/boxwarden/internal/clipboardx"
	"github.com/weshofmann/boxwarden/internal/execx"
	"github.com/weshofmann/boxwarden/internal/guestproto"
)

func clipboardStage(ctx context.Context, stage, status string) {
	clipboarddiag.Record(clipboarddiag.WithSource(ctx, "ssh"), stage, status)
}
func clipboardDiagnosticEnvelope(ctx context.Context, r guestproto.ClipboardRequest, encoded []byte) ([]byte, error) {
	o, _, ok := clipboarddiag.Get(ctx)
	if !ok {
		return encoded, nil
	}
	if !clipboarddiag.Available(ctx) || o.Domain != r.Domain || o.SessionID != r.SessionID || o.BackendKind != r.BackendKind || o.BackendObject != r.BackendObject || o.Generation != r.Generation || o.Direction != r.Direction || !o.ExpiresAt.Equal(r.ExpiresAt) {
		return nil, clipboardx.ErrAdmission
	}
	header, err := clipboarddiag.EncodeOperation(o)
	if err != nil {
		return nil, clipboardx.ErrAdmission
	}
	return append(append(header, '\n'), encoded...), nil
}
func clipboardHelperArguments(ctx context.Context, connection Connection) []string {
	args := fixedGuestHelperArguments(connection, "clipboard")
	if _, _, ok := clipboarddiag.Get(ctx); ok {
		args[len(args)-2] = guestproto.ClipboardDiagnosticHelperPath
		args[len(args)-1] = "invoke"
	}
	return args
}

// Collection's finite production drain is independent of the text runner.
func newClipboardDiagnosticCollectionRunner() ExecRunner {
	return newExecRunner(execx.OSRunner{MaxStdoutBytes: clipboarddiag.MaxCollectionBytes, MaxStderrBytes: guestproto.MaxClipboardMetadataBytes, MaxStdinBytes: clipboarddiag.MaxHeaderBytes, StrictStderr: true})
}
func (c *Client) CollectClipboardDiagnostic(ctx context.Context, connection Connection, o clipboarddiag.Operation) (clipboarddiag.GuestCollection, error) {
	return c.collectClipboardDiagnosticWith(ctx, connection, o, newClipboardDiagnosticCollectionRunner())
}
func (c *Client) collectClipboardDiagnosticWith(ctx context.Context, connection Connection, o clipboarddiag.Operation, runner Runner) (clipboarddiag.GuestCollection, error) {
	if c == nil || c.clipboardRunner == nil || o.Validate(false) != nil || validateConnection(connection) != nil || string(connection.Binding.Domain) != o.Domain || connection.Binding.SessionID != o.SessionID || connection.Binding.BackendKind != o.BackendKind || connection.Binding.BackendObject != o.BackendObject || ctx.Err() != nil || verifyKnownHostsPin(connection) != nil {
		return clipboarddiag.GuestCollection{}, clipboarddiag.ErrMetadata
	}
	header, err := clipboarddiag.EncodeOperation(o)
	if err != nil {
		return clipboarddiag.GuestCollection{}, clipboarddiag.ErrMetadata
	}
	args := fixedGuestHelperArguments(connection, "collect")
	args[len(args)-2] = guestproto.ClipboardDiagnosticHelperPath
	result, runErr := runner.Run(ctx, Command{Path: sshPath, Args: args, Stdin: header})
	if runErr != nil || ctx.Err() != nil || result.Truncated || len(result.Stdout) > clipboarddiag.MaxCollectionBytes || len(result.Stderr) > guestproto.MaxClipboardMetadataBytes {
		return clipboarddiag.GuestCollection{}, clipboarddiag.ErrMetadata
	}
	witness, proofErr := clipboarddiag.DecodePublication([]byte(result.Stderr), o, "collect")
	marker := []byte(clipboarddiag.HeaderDigest(o) + "\n")
	if !result.StderrComplete || proofErr != nil || *witness.Collect != clipboarddiag.MetadataDigest(marker) {
		return clipboarddiag.GuestCollection{}, clipboarddiag.ErrMetadata
	}
	var receipt clipboarddiag.GuestCollection
	if clipboarddiag.StrictDecode([]byte(result.Stdout), &receipt, clipboarddiag.MaxCollectionBytes) != nil || receipt.Validate(o) != nil {
		return clipboarddiag.GuestCollection{}, clipboarddiag.ErrMetadata
	}
	return receipt, nil
}

func clipboardDiagnosticReady(ctx context.Context) bool { return clipboarddiag.Available(ctx) }

func newClipboardDiagnosticInvokeRunner() ExecRunner {
	return newExecRunner(execx.OSRunner{MaxStdinBytes: guestproto.MaxClipboardRequestBytes, MaxStdoutBytes: guestproto.MaxClipboardResponseBytes, MaxStderrBytes: clipboarddiag.MaxPublicationBytes, StrictStderr: true})
}
func configureClipboardDiagnosticClient(c *Client) {
	c.clipboardDiagnosticRunner = newClipboardDiagnosticInvokeRunner()
}
func clipboardExchangeRunner(ctx context.Context, c *Client) Runner {
	if _, _, ok := clipboarddiag.Get(ctx); ok && c.clipboardDiagnosticRunner != nil {
		return c.clipboardDiagnosticRunner
	}
	return c.clipboardRunner
}
func clipboardObservePublication(ctx context.Context, result Result, runErr error) {
	o, _, ok := clipboarddiag.Get(ctx)
	if !ok || runErr != nil || ctx.Err() != nil || !result.StderrComplete || result.Truncated {
		return
	}
	w, e := clipboarddiag.DecodePublication([]byte(result.Stderr), o, "invoke")
	if e == nil {
		clipboarddiag.ObservePublication(ctx, w)
	}
}

// A diagnostic stderr overflow is metadata loss; authoritative stdout still
// undergoes the same ordinary ACK adjudication. Text overflow stays unchanged.
func clipboardTransferTruncated(ctx context.Context, r Result) bool {
	if _, _, ok := clipboarddiag.Get(ctx); ok && r.StderrTruncated {
		return r.StdoutTruncated
	}
	return r.Truncated
}
