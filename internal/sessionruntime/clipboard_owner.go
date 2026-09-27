package sessionruntime

import (
	"context"
	"errors"

	"github.com/weshofmann/boxwarden/internal/clipboardx"
	"github.com/weshofmann/boxwarden/internal/domain"
	"github.com/weshofmann/boxwarden/internal/guestproto"
	"github.com/weshofmann/boxwarden/internal/session"
	"github.com/weshofmann/boxwarden/internal/sshx"
	"github.com/weshofmann/boxwarden/internal/supervisor"
)

// clipboardClient errors ErrAdmission, ErrRequest and ErrCancelled prove refusal
// before write dispatch. Every possible post-dispatch write failure is ErrUnknown.
type clipboardClient interface {
	Clipboard(context.Context, sshx.Connection, guestproto.ClipboardRequest, []byte) (guestproto.ClipboardResponse, []byte, error)
}

var _ supervisor.ClipboardOwner = (*Owner)(nil)

type clipboardRuntime struct {
	client                 clipboardClient
	connection             sshx.Connection
	binding                supervisor.Binding
	stateRoot, sessionName string
}

// ReadClipboard returns complete validated text from only the retained runtime.
// No payload, journal, recipe action or credential is persisted by this operation.
func (o *Owner) ReadClipboard(ctx context.Context) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, clipboardx.TransferTimeout)
	defer cancel()
	runtime, err := o.admitClipboard(ctx)
	if err != nil {
		return nil, err
	}
	request := runtime.request(ctx, "read")
	response, data, err := runtime.client.Clipboard(ctx, runtime.connection, request, nil)
	if err != nil {
		return nil, clipboardx.ErrRead
	}
	if _, err = guestproto.EncodeClipboardResponse(request, response, data); err != nil {
		return nil, clipboardx.ErrRead
	}
	if !o.clipboardStillReady(ctx, runtime) {
		return nil, clipboardx.ErrRead
	}
	if response.Status != "ok" {
		return nil, clipboardx.ErrUnavailable
	}
	if err = clipboardx.Validate(data); err != nil {
		return nil, err
	}
	return data, nil
}

// WriteClipboard reports a confirmed claim only when its exact generation is
// still READY after the acknowledgement. Cancellation or drift after dispatch
// cannot promise that the old guest selection was preserved and never retries.
func (o *Owner) WriteClipboard(ctx context.Context, data []byte) (clipboardx.Outcome, error) {
	if err := clipboardx.Validate(data); err != nil {
		return clipboardx.Unchanged, err
	}
	ctx, cancel := context.WithTimeout(ctx, clipboardx.TransferTimeout)
	defer cancel()
	runtime, err := o.admitClipboard(ctx)
	if err != nil {
		return clipboardx.Unchanged, err
	}
	request := runtime.request(ctx, "write")
	response, payload, err := runtime.client.Clipboard(ctx, runtime.connection, request, data)
	if err != nil {
		for _, safe := range []error{clipboardx.ErrAdmission, clipboardx.ErrRequest, clipboardx.ErrCancelled} {
			if errors.Is(err, safe) {
				return clipboardx.Unchanged, safe
			}
		}
		return clipboardx.Unknown, clipboardx.ErrUnknown
	}
	if _, err = guestproto.EncodeClipboardResponse(request, response, payload); err != nil || (response.Status == "ok" && response.Length != len(data)) {
		return clipboardx.Unknown, clipboardx.ErrUnknown
	}
	if !o.clipboardStillReady(ctx, runtime) {
		return clipboardx.Unknown, clipboardx.ErrUnknown
	}
	switch response.Status {
	case "ok":
		return clipboardx.Committed, nil
	case "error":
		return clipboardx.Unchanged, clipboardx.ErrWrite
	default:
		return clipboardx.Unknown, clipboardx.ErrUnknown
	}
}

func (r clipboardRuntime) request(ctx context.Context, direction string) guestproto.ClipboardRequest {
	deadline, _ := ctx.Deadline()
	return guestproto.ClipboardRequest{Version: guestproto.Version, Association: guestproto.Association{Domain: r.binding.Domain, SessionID: r.binding.SessionID, BackendKind: r.binding.BackendKind, BackendObject: r.binding.BackendObject}, Generation: r.binding.Generation, Direction: direction, ExpiresAt: deadline.UTC()}
}

func (o *Owner) admitClipboard(ctx context.Context) (clipboardRuntime, error) {
	if ctx.Err() != nil {
		return clipboardRuntime{}, clipboardx.ErrCancelled
	}
	if o == nil || o.deps.client == nil {
		return clipboardRuntime{}, clipboardx.ErrAdmission
	}
	client, ok := o.deps.client.(clipboardClient)
	if !ok {
		return clipboardRuntime{}, clipboardx.ErrAdmission
	}
	before := o.Snapshot(ctx)
	if ctx.Err() != nil {
		return clipboardRuntime{}, clipboardx.ErrCancelled
	}
	if !readyImportSnapshot(before) {
		return clipboardRuntime{}, clipboardx.ErrAdmission
	}
	// Capture current credentials after the fresh observation. Holding readyMu
	// across SSH would delay maintenance; the binding and pin stay exact while
	// certificate renewal may continue using the existing owner mechanism.
	o.mu.Lock()
	runtime := clipboardRuntime{client: client, connection: o.connection, binding: o.binding, stateRoot: o.stateRoot, sessionName: o.sessionName}
	exact := o.clipboardConnectionMatchesLocked(runtime.connection, runtime.binding)
	o.mu.Unlock()
	if !exact || before.Binding != runtime.binding || admitClipboardRecord(runtime) != nil {
		return clipboardRuntime{}, clipboardx.ErrAdmission
	}
	if ctx.Err() != nil {
		return clipboardRuntime{}, clipboardx.ErrCancelled
	}
	return runtime, nil
}

func (o *Owner) clipboardConnectionMatchesLocked(conn sshx.Connection, binding supervisor.Binding) bool {
	return o.active && o.readyEstablished && o.binding == binding && conn.Binding == o.sshBinding &&
		string(conn.Binding.Domain) == binding.Domain && conn.Binding.SessionID == binding.SessionID &&
		conn.Binding.BackendKind == binding.BackendKind && conn.Binding.BackendObject == binding.BackendObject &&
		conn.RuntimeDirectory == o.runtimePath && conn.Pin == o.expectedPin
}

func (o *Owner) clipboardStillReady(ctx context.Context, runtime clipboardRuntime) bool {
	if ctx.Err() != nil {
		return false
	}
	after := o.Snapshot(ctx)
	if ctx.Err() != nil || after.Binding != runtime.binding || !readyImportSnapshot(after) {
		return false
	}
	o.mu.Lock()
	exact := o.clipboardConnectionMatchesLocked(o.connection, runtime.binding) && o.stateRoot == runtime.stateRoot && o.sessionName == runtime.sessionName
	o.mu.Unlock()
	return exact && admitClipboardRecord(runtime) == nil && ctx.Err() == nil
}

func admitClipboardRecord(runtime clipboardRuntime) error {
	if runtime.stateRoot == "" || runtime.sessionName == "" {
		return clipboardx.ErrAdmission
	}
	binding := runtime.binding
	domainID, err := domain.Parse(binding.Domain)
	if err != nil {
		return clipboardx.ErrAdmission
	}
	record, err := session.LoadRecord(runtime.stateRoot, binding.Domain, runtime.sessionName)
	if err != nil || record.Domain != domainID || string(record.Name) != runtime.sessionName || record.ID != binding.SessionID ||
		record.Backend.Kind != binding.BackendKind || record.Backend.ObjectID != binding.BackendObject || record.StartGeneration != binding.Generation ||
		record.IntendedState != session.StateRunning || record.Readiness.Status != session.ReadinessReady {
		return clipboardx.ErrAdmission
	}
	if session.RequireNoRebuild(runtime.stateRoot, domainID, runtime.sessionName) != nil {
		return clipboardx.ErrAdmission
	}
	return nil
}
