//go:build !n1clipboarddiagnostic

package guestproto

import (
	"context"
	"errors"
)

// Preserve the frozen generic helper historical source map after this exact body move.
//
//line clipboard.go:378
func (b *Bootstrapper) Clipboard(ctx context.Context, request ClipboardRequest, payload []byte) (ClipboardResponse, []byte, error) {
	if request.Validate() != nil || ValidateClipboardText(payload) != nil || (request.Direction == "read" && len(payload) != 0) {
		return ClipboardResponse{}, nil, errClipboardFrame
	}
	if ctx.Err() != nil || b == nil || b.ClipboardExecutor == nil || b.checkClipboardBinding(request) != nil {
		return ClipboardResponse{}, nil, errors.New("clipboard target unavailable")
	}
	operationCtx, cancel := context.WithDeadline(ctx, request.ExpiresAt)
	defer cancel()
	raw, runErr := b.ClipboardExecutor.Run(operationCtx, request.Direction, payload)
	response, data, decodeErr := decodeDesktopReceipt(request, raw)
	if request.Direction == "write" {
		// An explicit bounded precommit refusal remains a refusal. Missing/malformed
		// acknowledgement, cancellation or changed binding after dispatch is unknown.
		if decodeErr == nil && response.Status == "error" {
			return response, nil, nil
		}
		if runErr != nil || decodeErr != nil || (response.Status == "ok" && response.Length != len(payload)) || b.checkClipboardBinding(request) != nil {
			return ClipboardResponse{Version: Version, Association: request.Association, Generation: request.Generation, Status: "unknown"}, nil, nil
		}
		return response, nil, nil
	}
	if runErr != nil || decodeErr != nil || operationCtx.Err() != nil || b.checkClipboardBinding(request) != nil {
		return ClipboardResponse{}, nil, errors.New("clipboard read unavailable")
	}
	return response, data, nil
}
