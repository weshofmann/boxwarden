package session

import (
	"context"
	"github.com/weshofmann/boxwarden/internal/clipboardx"
	"github.com/weshofmann/boxwarden/internal/config"
	"github.com/weshofmann/boxwarden/internal/lock"
	"io"
)

// ClipboardService retains lifecycle exclusion across exact target capture,
// supervisor admission, source capture and destination acknowledgement.
type ClipboardService struct {
	domain   config.Domain
	endpoint clipboardx.Endpoint
}

func NewClipboardService(d config.Domain, endpoint clipboardx.Endpoint) *ClipboardService {
	return &ClipboardService{domain: d, endpoint: endpoint}
}
func (s *ClipboardService) Execute(ctx context.Context, rawName string, request clipboardx.Request, input io.Reader, output io.Writer, board clipboardx.Pasteboard) (clipboardx.Outcome, error) {
	if s == nil || s.endpoint == nil {
		return clipboardx.Unchanged, clipboardx.ErrAdmission
	}
	name, err := ParseName(rawName)
	if err != nil {
		return clipboardx.Unchanged, clipboardx.ErrRequest
	}
	d := string(s.domain.ID)
	transition, err := lock.TryAcquire(ctx, s.domain.StateRoot, "transition-"+d+"-"+string(name))
	if err != nil {
		return clipboardx.Unchanged, clipboardx.ErrAdmission
	}
	defer transition.Release()
	held, err := lock.TryAcquire(ctx, s.domain.StateRoot, "session-"+d+"-"+string(name))
	if err != nil {
		return clipboardx.Unchanged, clipboardx.ErrAdmission
	}
	defer held.Release()
	record, err := LoadRecord(s.domain.StateRoot, d, string(name))
	if err != nil || record.IntendedState != StateRunning || record.Readiness.Status != ReadinessReady || !validUUID(record.StartGeneration) || RequireNoRebuild(s.domain.StateRoot, s.domain.ID, string(name)) != nil {
		return clipboardx.Unchanged, clipboardx.ErrAdmission
	}
	target := clipboardx.Target{Domain: d, SessionID: record.ID, BackendKind: record.Backend.Kind, BackendObject: record.Backend.ObjectID, Generation: record.StartGeneration}
	if request.Target != (clipboardx.Target{}) && request.Target != target {
		return clipboardx.Unchanged, clipboardx.ErrAdmission
	}
	request.Target = target
	return (clipboardx.Service{Endpoint: s.endpoint}).Execute(ctx, request, input, output, board)
}
