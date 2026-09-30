//go:build n1clipboarddiagnostic

package guestproto

import (
	"context"
	"errors"
	"github.com/weshofmann/boxwarden/internal/clipboarddiag"
)

func diagnosticRequestMatches(o clipboarddiag.Operation, r ClipboardRequest) bool {
	return o.Domain == r.Domain && o.SessionID == r.SessionID && o.BackendKind == r.BackendKind && o.BackendObject == r.BackendObject && o.Generation == r.Generation && o.Direction == r.Direction && o.ExpiresAt.Equal(r.ExpiresAt)
}

// Clipboard evaluates each original short-circuit exactly once. Diagnostic loss
// after dispatch invalidates evidence without replacing the original outcome.
func (b *Bootstrapper) Clipboard(ctx context.Context, request ClipboardRequest, payload []byte) (ClipboardResponse, []byte, error) {
	ctx = clipboarddiag.WithSource(ctx, "guest")
	fail := func(stage string) (ClipboardResponse, []byte, error) {
		clipboarddiag.Record(ctx, stage, "refused")
		return ClipboardResponse{}, nil, errClipboardFrame
	}
	if request.Validate() != nil {
		return fail("guest_request")
	}
	if ValidateClipboardText(payload) != nil {
		return fail("guest_text")
	}
	if request.Direction == "read" && len(payload) != 0 {
		return fail("guest_read_payload")
	}
	unavailable := func(stage string) (ClipboardResponse, []byte, error) {
		clipboarddiag.Record(ctx, stage, "unavailable")
		return ClipboardResponse{}, nil, errors.New("clipboard target unavailable")
	}
	if ctx.Err() != nil {
		return unavailable("guest_context")
	}
	if b == nil {
		return unavailable("guest_receiver")
	}
	if b.ClipboardExecutor == nil {
		return unavailable("guest_executor")
	}
	if b.checkClipboardBinding(request) != nil {
		return unavailable("guest_prebinding")
	}
	if op, _, ok := clipboarddiag.Get(ctx); ok && (!diagnosticRequestMatches(op, request) || !clipboarddiag.Available(ctx)) {
		return unavailable("helper_metadata")
	}
	clipboarddiag.Record(ctx, "guest_admitted", "ok")
	if !clipboarddiag.Available(ctx) {
		return unavailable("helper_metadata")
	}
	operationCtx, cancel := context.WithDeadline(ctx, request.ExpiresAt)
	defer cancel()
	clipboarddiag.Record(ctx, "guest_dispatch", "ok")
	if !clipboarddiag.Available(ctx) {
		return unavailable("helper_metadata")
	}
	raw, runErr := b.ClipboardExecutor.Run(operationCtx, request.Direction, payload)
	response, data, decodeErr := decodeDesktopReceipt(request, raw)
	decodeStage := "guest_decode_malformed"
	if len(raw) == 0 {
		decodeStage = "guest_decode_missing"
	}
	if request.Direction == "write" {
		if decodeErr == nil && response.Status == "error" {
			clipboarddiag.Record(ctx, "guest_explicit_error", "refused")
			return response, nil, nil
		}
		unknown := func(stage string) (ClipboardResponse, []byte, error) {
			clipboarddiag.Record(ctx, stage, "unknown")
			return ClipboardResponse{Version: Version, Association: request.Association, Generation: request.Generation, Status: "unknown"}, nil, nil
		}
		if runErr != nil {
			return unknown("guest_execution")
		}
		if decodeErr != nil {
			return unknown(decodeStage)
		}
		if response.Status == "ok" && response.Length != len(payload) {
			return unknown("guest_length")
		}
		if b.checkClipboardBinding(request) != nil {
			return unknown("guest_postbinding")
		}
		if response.Status == "unknown" {
			clipboarddiag.Record(ctx, "guest_unknown", "unknown")
		} else {
			clipboarddiag.Record(ctx, "guest_complete", "ok")
		}
		return response, nil, nil
	}
	readFailed := func(stage string) (ClipboardResponse, []byte, error) {
		clipboarddiag.Record(ctx, stage, "unavailable")
		return ClipboardResponse{}, nil, errors.New("clipboard read unavailable")
	}
	if runErr != nil {
		return readFailed("guest_execution")
	}
	if decodeErr != nil {
		return readFailed(decodeStage)
	}
	if operationCtx.Err() != nil {
		return readFailed("guest_post_context")
	}
	if b.checkClipboardBinding(request) != nil {
		return readFailed("guest_postbinding")
	}
	clipboarddiag.Record(ctx, "guest_complete", "ok")
	return response, data, nil
}
