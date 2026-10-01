//go:build n1clipboarddiagnostic && !n1candidate

package sessionruntime

import (
	"context"
	"github.com/weshofmann/boxwarden/internal/clipboarddiag"
	"github.com/weshofmann/boxwarden/internal/sshx"
)

func clipboardStage(ctx context.Context, stage, status string) {
	clipboarddiag.Record(clipboarddiag.WithSource(ctx, "runtime"), stage, status)
}
func clipboardDiagnosticAdmission(ctx context.Context, r clipboardRuntime, direction string) bool {
	o, _, ok := clipboarddiag.Get(ctx)
	return !ok || (clipboarddiag.Available(ctx) && o.Domain == r.binding.Domain && o.SessionID == r.binding.SessionID && o.BackendKind == r.binding.BackendKind && o.BackendObject == r.binding.BackendObject && o.Generation == r.binding.Generation && o.Direction == direction && o.ExpiresAt.Equal(r.request(ctx, direction).ExpiresAt))
}
func (o *Owner) CollectClipboardDiagnostic(ctx context.Context, operation clipboarddiag.Operation) (clipboarddiag.GuestCollection, error) {
	if operation.Validate(false) != nil {
		return clipboarddiag.GuestCollection{}, clipboarddiag.ErrMetadata
	}
	runtime, err := o.admitClipboard(ctx)
	if err != nil {
		return clipboarddiag.GuestCollection{}, clipboarddiag.ErrMetadata
	}
	if operation.Domain != runtime.binding.Domain || operation.SessionID != runtime.binding.SessionID || operation.BackendKind != runtime.binding.BackendKind || operation.BackendObject != runtime.binding.BackendObject || operation.Generation != runtime.binding.Generation {
		return clipboarddiag.GuestCollection{}, clipboarddiag.ErrMetadata
	}
	collector, ok := runtime.client.(interface {
		CollectClipboardDiagnostic(context.Context, sshx.Connection, clipboarddiag.Operation) (clipboarddiag.GuestCollection, error)
	})
	if !ok {
		return clipboarddiag.GuestCollection{}, clipboarddiag.ErrMetadata
	}
	result, err := collector.CollectClipboardDiagnostic(ctx, runtime.connection, operation)
	if err != nil || !o.clipboardStillReady(ctx, runtime) || result.Validate(operation) != nil {
		return clipboarddiag.GuestCollection{}, clipboarddiag.ErrMetadata
	}
	return result, nil
}
