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
	if !clipboardDiagnosticAdmission(ctx, runtime, "read") {
		clipboardStage(ctx, "runtime_binding", "refused")
		return nil, clipboardx.ErrAdmission
	}
	request := runtime.request(ctx, "read")
	clipboardStage(ctx, "runtime_dispatch", "ok")
	response, data, err := runtime.client.Clipboard(ctx, runtime.connection, request, nil)
	if err != nil {
		clipboardStage(ctx, "runtime_transport", "unavailable")
		return nil, clipboardx.ErrRead
	}
	if _, err = guestproto.EncodeClipboardResponse(request, response, data); err != nil {
		clipboardStage(ctx, "runtime_response", "unavailable")
		return nil, clipboardx.ErrRead
	}
	if !o.clipboardStillReady(ctx, runtime) {
		return nil, clipboardx.ErrRead
	}
	if response.Status != "ok" {
		clipboardStage(ctx, "runtime_status", "unavailable")
		return nil, clipboardx.ErrUnavailable
	}
	if err = clipboardx.Validate(data); err != nil {
		clipboardStage(ctx, "runtime_text", "unavailable")
		return nil, err
	}
	clipboardStage(ctx, "runtime_complete", "ok")
	return data, nil
}

// WriteClipboard reports a confirmed claim only when its exact generation is
// still READY after the acknowledgement. Cancellation or drift after dispatch
// cannot promise that the old guest selection was preserved and never retries.
func (o *Owner) WriteClipboard(ctx context.Context, data []byte) (clipboardx.Outcome, error) {
	if err := clipboardx.Validate(data); err != nil {
		clipboardStage(ctx, "runtime_text", "refused")
		return clipboardx.Unchanged, err
	}
	ctx, cancel := context.WithTimeout(ctx, clipboardx.TransferTimeout)
	defer cancel()
	runtime, err := o.admitClipboard(ctx)
	if err != nil {
		return clipboardx.Unchanged, err
	}
	if !clipboardDiagnosticAdmission(ctx, runtime, "write") {
		clipboardStage(ctx, "runtime_binding", "refused")
		return clipboardx.Unchanged, clipboardx.ErrAdmission
	}
	request := runtime.request(ctx, "write")
	clipboardStage(ctx, "runtime_dispatch", "ok")
	response, payload, err := runtime.client.Clipboard(ctx, runtime.connection, request, data)
	if err != nil {
		for _, safe := range []error{clipboardx.ErrAdmission, clipboardx.ErrRequest, clipboardx.ErrCancelled} {
			if errors.Is(err, safe) {
				clipboardStage(ctx, "runtime_transport", "refused")
				return clipboardx.Unchanged, safe
			}
		}
		clipboardStage(ctx, "runtime_transport", "unknown")
		return clipboardx.Unknown, clipboardx.ErrUnknown
	}
	if _, err = guestproto.EncodeClipboardResponse(request, response, payload); err != nil {
		clipboardStage(ctx, "runtime_response", "unknown")
		return clipboardx.Unknown, clipboardx.ErrUnknown
	}
	if response.Status == "ok" && response.Length != len(data) {
		clipboardStage(ctx, "runtime_length", "unknown")
		return clipboardx.Unknown, clipboardx.ErrUnknown
	}
	if !o.clipboardStillReady(ctx, runtime) {
		return clipboardx.Unknown, clipboardx.ErrUnknown
	}
	switch response.Status {
	case "ok":
		clipboardStage(ctx, "runtime_complete", "ok")
		return clipboardx.Committed, nil
	case "error":
		clipboardStage(ctx, "runtime_status", "refused")
		return clipboardx.Unchanged, clipboardx.ErrWrite
	default:
		clipboardStage(ctx, "runtime_status", "unknown")
		return clipboardx.Unknown, clipboardx.ErrUnknown
	}
}

func (r clipboardRuntime) request(ctx context.Context, direction string) guestproto.ClipboardRequest {
	deadline, _ := ctx.Deadline()
	return guestproto.ClipboardRequest{Version: guestproto.Version, Association: guestproto.Association{Domain: r.binding.Domain, SessionID: r.binding.SessionID, BackendKind: r.binding.BackendKind, BackendObject: r.binding.BackendObject}, Generation: r.binding.Generation, Direction: direction, ExpiresAt: deadline.UTC()}
}

func (o *Owner) admitClipboard(ctx context.Context) (clipboardRuntime, error) {
	refuse := func(stage string, err error) (clipboardRuntime, error) {
		clipboardStage(ctx, stage, "refused")
		return clipboardRuntime{}, err
	}
	if ctx.Err() != nil {
		return refuse("runtime_context", clipboardx.ErrCancelled)
	}
	if o == nil {
		return refuse("runtime_owner", clipboardx.ErrAdmission)
	}
	if o.deps.client == nil {
		return refuse("runtime_client_missing", clipboardx.ErrAdmission)
	}
	client, ok := o.deps.client.(clipboardClient)
	if !ok {
		return refuse("runtime_client", clipboardx.ErrAdmission)
	}
	before := o.Snapshot(ctx)
	if ctx.Err() != nil {
		return refuse("runtime_observation_context", clipboardx.ErrCancelled)
	}
	if !readyImportSnapshot(before) {
		return refuse("runtime_ready", clipboardx.ErrAdmission)
	}
	o.mu.Lock()
	runtime := clipboardRuntime{client: client, connection: o.connection, binding: o.binding, stateRoot: o.stateRoot, sessionName: o.sessionName}
	exact := o.clipboardConnectionMatchesLocked(runtime.connection, runtime.binding)
	o.mu.Unlock()
	if !exact {
		return refuse("runtime_connection", clipboardx.ErrAdmission)
	}
	if before.Binding != runtime.binding {
		return refuse("runtime_binding", clipboardx.ErrAdmission)
	}
	if admitClipboardRecord(runtime) != nil {
		return refuse("runtime_record", clipboardx.ErrAdmission)
	}
	if ctx.Err() != nil {
		return refuse("runtime_final_context", clipboardx.ErrCancelled)
	}
	clipboardStage(ctx, "runtime_admitted", "ok")
	return runtime, nil
}

func (o *Owner) clipboardConnectionMatchesLocked(conn sshx.Connection, binding supervisor.Binding) bool {
	return o.active && o.readyEstablished && o.binding == binding && conn.Binding == o.sshBinding &&
		string(conn.Binding.Domain) == binding.Domain && conn.Binding.SessionID == binding.SessionID &&
		conn.Binding.BackendKind == binding.BackendKind && conn.Binding.BackendObject == binding.BackendObject &&
		conn.RuntimeDirectory == o.runtimePath && conn.Pin == o.expectedPin
}

func (o *Owner) clipboardStillReady(ctx context.Context, runtime clipboardRuntime) bool {
	fail := func(stage string) bool { clipboardStage(ctx, stage, "unavailable"); return false }
	if ctx.Err() != nil {
		return fail("runtime_post_context")
	}
	after := o.Snapshot(ctx)
	if ctx.Err() != nil {
		return fail("runtime_post_context")
	}
	if after.Binding != runtime.binding {
		return fail("runtime_post_binding")
	}
	if !readyImportSnapshot(after) {
		return fail("runtime_post_ready")
	}
	o.mu.Lock()
	exact := o.clipboardConnectionMatchesLocked(o.connection, runtime.binding) && o.stateRoot == runtime.stateRoot && o.sessionName == runtime.sessionName
	o.mu.Unlock()
	if !exact {
		return fail("runtime_post_connection")
	}
	if admitClipboardRecord(runtime) != nil {
		return fail("runtime_post_record")
	}
	if ctx.Err() != nil {
		return fail("runtime_post_context")
	}
	return true
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
