//go:build n1clipboarddiagnostic && !n1candidate

package session

import (
	"bytes"
	"context"
	"github.com/weshofmann/boxwarden/internal/clipboarddiag"
	"github.com/weshofmann/boxwarden/internal/clipboardx"
	"github.com/weshofmann/boxwarden/internal/config"
	"time"
)

const ClipboardDiagnosticFixtureID = "n1_clipboard_control_v1"
const ClipboardDiagnosticFixture = "boxwarden-n1-clipboard-control\n"
const ClipboardDiagnosticFixtureSHA256 = "9096af926f38bc69facaa4d383ba789f106a6506fadf6cedd170616b882f88e7"

type ClipboardDiagnosticEndpoint interface {
	clipboardx.Endpoint
	Collect(context.Context, clipboarddiag.Operation, clipboarddiag.Fragment) (clipboarddiag.CollectionReceipt, error)
}
type ClipboardDiagnosticService struct {
	ordinary *ClipboardService
	endpoint ClipboardDiagnosticEndpoint
}

func NewClipboardDiagnosticService(d config.Domain, endpoint ClipboardDiagnosticEndpoint) (*ClipboardDiagnosticService, error) {
	if string(d.ID) != "n1qualification" || endpoint == nil {
		return nil, clipboardx.ErrAdmission
	}
	return &ClipboardDiagnosticService{NewClipboardService(d, endpoint), endpoint}, nil
}

// Invoke retains the actual ordinary transfer outcome. Synthetic.Length is the
// fixed source bytes consumed for write, or conditional result bytes consumed
// for read; equality never becomes true solely because metadata is complete.
func (s *ClipboardDiagnosticService) Invoke(ctx context.Context, name string, o clipboarddiag.Operation) (clipboarddiag.InvocationReceipt, error) {
	if s == nil || s.ordinary == nil || o.Validate(true) != nil {
		return clipboarddiag.InvocationReceipt{}, clipboardx.ErrRequest
	}
	r, err := clipboarddiag.NewRecorder(o, "cli", time.Now)
	if err != nil {
		return clipboarddiag.InvocationReceipt{}, clipboardx.ErrRequest
	}
	ctx, err = clipboarddiag.WithContext(ctx, o, r)
	if err != nil {
		return clipboarddiag.InvocationReceipt{}, clipboardx.ErrRequest
	}
	ctx, cancel := context.WithDeadline(ctx, o.ExpiresAt)
	defer cancel()
	clipboarddiag.Record(ctx, "cli_binding", "ok")
	target := clipboardx.Target{Domain: o.Domain, SessionID: o.SessionID, BackendKind: o.BackendKind, BackendObject: o.BackendObject, Generation: o.Generation}
	request := clipboardx.Request{Target: target, Mode: clipboardx.Copy}
	var read bytes.Buffer
	if o.Direction == "read" {
		request.Mode = clipboardx.Paste
	}
	source := bytes.NewReader([]byte(ClipboardDiagnosticFixture))
	outcome, transferErr := s.ordinary.Execute(ctx, name, request, source, &read, nil)
	result := clipboarddiag.SyntheticResult{FixtureID: ClipboardDiagnosticFixtureID, ExpectedSHA256: ClipboardDiagnosticFixtureSHA256, Length: len(ClipboardDiagnosticFixture) - source.Len(), Equal: outcome == clipboardx.Committed}
	if o.Direction == "read" {
		result.Length = read.Len()
		result.Equal = bytes.Equal(read.Bytes(), []byte(ClipboardDiagnosticFixture)) && outcome == clipboardx.Committed
	}
	if transferErr != nil {
		status := "unknown"
		if outcome == clipboardx.Unchanged {
			status = "refused"
		}
		if outcome == clipboardx.Committed {
			status = "incomplete"
		}
		clipboarddiag.Record(ctx, "cli_outcome", status)
	} else {
		clipboarddiag.Record(ctx, "cli_outcome", "ok")
	}
	receipt := clipboarddiag.InvocationReceipt{Version: 1, Binding: o, Outcome: string(outcome), Complete: false, Fragments: []clipboarddiag.Fragment{r.Fragment()}, Synthetic: result}
	if receipt.Validate() != nil {
		return clipboarddiag.InvocationReceipt{}, clipboarddiag.ErrMetadata
	}
	// Safe typed outcomes carry transfer failure. Never expose arbitrary errors.
	return receipt, nil
}

// Collect uses the same real lifecycle admission/locks without retrieving text.
// Complete is metadata coverage; qualification must separately adjudicate the
// invocation outcome and synthetic result, including an unknown acknowledgement.
func (s *ClipboardDiagnosticService) Collect(ctx context.Context, name string, o clipboarddiag.Operation, cli clipboarddiag.Fragment) (clipboarddiag.CollectionReceipt, error) {
	if s == nil || o.Validate(false) != nil || cli.Validate() != nil || cli.Binding != o || cli.Origin != "cli" {
		return clipboarddiag.CollectionReceipt{}, clipboardx.ErrRequest
	}
	target := clipboardx.Target{Domain: o.Domain, SessionID: o.SessionID, BackendKind: o.BackendKind, BackendObject: o.BackendObject, Generation: o.Generation}
	var receipt clipboarddiag.CollectionReceipt
	err := s.ordinary.withClipboardTarget(ctx, name, target, func(current clipboardx.Target) error {
		var e error
		receipt, e = s.endpoint.Collect(ctx, o, cli)
		return e
	})
	if err != nil {
		return clipboarddiag.CollectionReceipt{}, clipboarddiag.ErrMetadata
	}
	return receipt, nil
}
