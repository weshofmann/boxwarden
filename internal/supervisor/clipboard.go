package supervisor

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net"
	"path/filepath"
	"sync"
	"time"

	"github.com/weshofmann/boxwarden/internal/clipboardx"
)

// ClipboardOwner is a narrow capability; it has no action journal or generic exec.
type ClipboardOwner interface {
	ReadClipboard(context.Context) ([]byte, error)
	WriteClipboard(context.Context, []byte) (clipboardx.Outcome, error)
}
type clipboardControlReply struct {
	Version int                `json:"version"`
	Binding Binding            `json:"binding"`
	Status  string             `json:"status"`
	Outcome clipboardx.Outcome `json:"outcome"`
	Length  int                `json:"length"`
}

func sendClipboardReply(c net.Conn, b Binding, status string, outcome clipboardx.Outcome, text []byte) error {
	reply := clipboardControlReply{Version: 1, Binding: b, Status: status, Outcome: outcome, Length: len(text)}
	data, err := json.Marshal(reply)
	if err != nil {
		return err
	}
	if err = writeFrame(c, data); err != nil {
		return err
	}
	if len(text) > 0 {
		_, err = io.Copy(c, bytes.NewReader(text))
	}
	return err
}

func handleClipboardControl(parent context.Context, c net.Conn, b Binding, owner RuntimeOwner, request controlRequest, accepted time.Time) {
	deadline := accepted.Add(clipboardx.TransferTimeout)
	if request.ExpiresAt.IsZero() {
		clipboardStage(parent, "control_expiry_missing", "refused")
		clipboardControlLoss(parent)
		return
	}
	if !request.ExpiresAt.After(time.Now()) {
		clipboardStage(parent, "control_expiry_elapsed", "refused")
		clipboardControlLoss(parent)
		return
	}
	if request.ExpiresAt.After(deadline) {
		clipboardStage(parent, "control_expiry_bound", "refused")
		clipboardControlLoss(parent)
		return
	}
	deadline = request.ExpiresAt
	if d, ok := parent.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	if c.SetDeadline(deadline) != nil {
		clipboardStage(parent, "control_deadline", "refused")
		clipboardControlLoss(parent)
		return
	}
	ctx, cancel := context.WithDeadline(parent, deadline)
	defer cancel()
	stopIO := context.AfterFunc(ctx, func() { c.SetDeadline(time.Now()) })
	defer stopIO()
	reply := func(status string, outcome clipboardx.Outcome, text []byte, stage string) error {
		clipboardDiagnosticOutcome(ctx, outcome)
		err := sendClipboardReply(c, b, status, outcome, text)
		if err != nil {
			clipboardStage(ctx, stage, "incomplete")
			clipboardControlLoss(ctx)
		} else {
			clipboardStage(ctx, stage, "ok")
		}
		return err
	}
	capability, ok := owner.(ClipboardOwner)
	before := owner.Snapshot(ctx)
	if !ok {
		clipboardStage(ctx, "control_capability", "refused")
		reply("error", clipboardx.Unchanged, nil, "control_final_error_frame")
		return
	}
	if ctx.Err() != nil {
		clipboardStage(ctx, "control_context", "cancelled")
		reply("error", clipboardx.Unchanged, nil, "control_final_error_frame")
		return
	}
	if before.Binding != b {
		clipboardStage(ctx, "control_binding", "refused")
		reply("error", clipboardx.Unchanged, nil, "control_final_error_frame")
		return
	}
	if !snapshotReady(before) {
		clipboardStage(ctx, "control_ready", "refused")
		reply("error", clipboardx.Unchanged, nil, "control_final_error_frame")
		return
	}
	if reply("ready", clipboardx.Unchanged, nil, "control_ready_frame") != nil {
		return
	}
	var text []byte
	if request.Action == "clipboard_write" {
		var n uint32
		if binary.Read(c, binary.BigEndian, &n) != nil {
			clipboardStage(ctx, "control_length", "unavailable")
			clipboardControlLoss(ctx)
			return
		}
		if n > clipboardx.MaxTextBytes {
			clipboardStage(ctx, "control_length", "refused")
			clipboardControlLoss(ctx)
			return
		}
		text = make([]byte, int(n))
		if _, err := io.ReadFull(c, text); err != nil {
			clipboardStage(ctx, "control_source", "unavailable")
			clipboardControlLoss(ctx)
			return
		}
		if clipboardx.Validate(text) != nil {
			clipboardStage(ctx, "control_source", "refused")
			clipboardControlLoss(ctx)
			return
		}
	}
	var terminator [1]byte
	if _, err := io.ReadFull(c, terminator[:]); err != nil {
		clipboardStage(ctx, "control_terminator", "unavailable")
		clipboardControlLoss(ctx)
		return
	}
	if terminator[0] != 0 {
		clipboardStage(ctx, "control_terminator", "refused")
		clipboardControlLoss(ctx)
		return
	}
	if ctx.Err() != nil {
		clipboardStage(ctx, "control_redispatch_context", "cancelled")
		clipboardControlLoss(ctx)
		return
	}
	disconnected := make(chan struct{})
	go func() { var extra [1]byte; _, _ = c.Read(extra[:]); cancel(); close(disconnected) }()
	defer func() { c.Close(); <-disconnected }()
	before = owner.Snapshot(ctx)
	if ctx.Err() != nil {
		clipboardStage(ctx, "control_redispatch_context", "cancelled")
		reply("error", clipboardx.Unchanged, nil, "control_final_error_frame")
		return
	}
	if before.Binding != b {
		clipboardStage(ctx, "control_redispatch_binding", "refused")
		reply("error", clipboardx.Unchanged, nil, "control_final_error_frame")
		return
	}
	if !snapshotReady(before) {
		clipboardStage(ctx, "control_redispatch_ready", "refused")
		reply("error", clipboardx.Unchanged, nil, "control_final_error_frame")
		return
	}
	outcome := clipboardx.Unchanged
	var err error
	clipboardStage(ctx, "control_dispatch", "ok")
	if request.Action == "clipboard_write" {
		outcome, err = capability.WriteClipboard(ctx, text)
		if outcome != clipboardx.Unchanged && outcome != clipboardx.Committed && outcome != clipboardx.Unknown {
			clipboardStage(ctx, "control_outcome", "unknown")
			outcome = clipboardx.Unknown
			err = clipboardx.ErrUnknown
		}
	} else {
		text, err = capability.ReadClipboard(ctx)
		if err == nil {
			err = clipboardx.Validate(text)
			if err != nil {
				clipboardStage(ctx, "control_read_validation", "unavailable")
			}
		}
	}
	after := owner.Snapshot(ctx)
	drift := ""
	if ctx.Err() != nil {
		drift = "control_post_context"
	} else if after.Binding != b {
		drift = "control_post_binding"
	} else if !snapshotReady(after) {
		drift = "control_post_ready"
	}
	if drift != "" {
		clipboardStage(ctx, drift, "unavailable")
		if request.Action == "clipboard_write" && outcome != clipboardx.Unchanged {
			outcome = clipboardx.Unknown
		}
		err = clipboardx.ErrAdmission
	}
	if err != nil || outcome == clipboardx.Unknown {
		reply("error", outcome, nil, "control_final_error_frame")
		return
	}
	if request.Action == "clipboard_write" {
		text = nil
	}
	reply("ok", outcome, text, "control_final_ok_frame")
}

// ExactClipboardController admits only an existing live generation. Begin sends
// metadata and waits for fresh readiness before its caller reads any source.
type ExactClipboardController struct{ runtimeRoot string }

func NewExactClipboardController(root string) (*ExactClipboardController, error) {
	if !canonicalAbsolute(root) {
		return nil, clipboardx.ErrAdmission
	}
	return &ExactClipboardController{runtimeRoot: root}, nil
}
func (c *ExactClipboardController) Begin(ctx context.Context, target clipboardx.Target, direction clipboardx.Direction) (clipboardx.Transfer, error) {
	if c == nil {
		return nil, clipboardx.ErrAdmission
	}
	b := Binding{Domain: target.Domain, SessionID: target.SessionID, BackendKind: target.BackendKind, BackendObject: target.BackendObject, Generation: target.Generation}
	dir, err := exactRuntimeDirectory(c.runtimeRoot, b)
	if err != nil {
		return nil, clipboardx.ErrAdmission
	}
	return (&Client{RuntimeDirectory: dir}).beginClipboard(ctx, b, direction)
}
func (c *Client) beginClipboard(parent context.Context, b Binding, direction clipboardx.Direction) (clipboardx.Transfer, error) {
	if c == nil || !b.valid() || parent.Err() != nil {
		return nil, clipboardx.ErrAdmission
	}
	action := "clipboard_read"
	if direction == clipboardx.ToGuest {
		action = "clipboard_write"
	} else if direction != clipboardx.FromGuest {
		return nil, clipboardx.ErrRequest
	}
	request, err := readLaunchRequest(filepath.Join(c.RuntimeDirectory, requestName))
	if err != nil || request.Binding != b {
		return nil, clipboardx.ErrAdmission
	}
	if validateGenerationEntry(c.RuntimeDirectory, socketName) != nil {
		return nil, clipboardx.ErrAdmission
	}
	state, err := classifyExactGeneration(request)
	if err != nil || state != exactGenerationLive {
		return nil, clipboardx.ErrAdmission
	}
	ctx, cancel := context.WithTimeout(parent, clipboardx.TransferTimeout)
	conn, err := dialControl(ctx, filepath.Join(c.RuntimeDirectory, socketName))
	if err != nil {
		cancel()
		return nil, clipboardx.ErrAdmission
	}
	t := &clipboardConnection{conn: conn, binding: b, direction: direction, ctx: ctx, cancel: cancel}
	t.stopCancel = context.AfterFunc(ctx, func() { conn.Close() })
	deadline, _ := ctx.Deadline()
	handshake := time.Now().Add(controlIOTimeout)
	if deadline.Before(handshake) {
		handshake = deadline
	}
	conn.SetDeadline(handshake)
	data, requestErr := clipboardBeginRequest(ctx, b, action, deadline.UTC())
	if requestErr != nil {
		t.Close()
		return nil, clipboardx.ErrAdmission
	}
	if err = writeFrame(conn, data); err != nil {
		t.Close()
		return nil, clipboardx.ErrAdmission
	}
	reply, err := t.readReply()
	if err != nil || reply.Status != "ready" || reply.Outcome != clipboardx.Unchanged || reply.Length != 0 {
		t.Close()
		return nil, clipboardx.ErrAdmission
	}
	if ctx.Err() != nil || conn.SetDeadline(deadline) != nil {
		t.Close()
		return nil, clipboardx.ErrAdmission
	}
	return t, nil
}

type clipboardConnection struct {
	conn       net.Conn
	binding    Binding
	direction  clipboardx.Direction
	ctx        context.Context
	cancel     context.CancelFunc
	stopCancel func() bool
	mu         sync.Mutex
	used       bool
	once       sync.Once
}

func (t *clipboardConnection) Close() error {
	var err error
	t.once.Do(func() {
		if t.stopCancel != nil {
			t.stopCancel()
		}
		t.cancel()
		err = t.conn.Close()
	})
	return err
}
func (t *clipboardConnection) claim(direction clipboardx.Direction) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.used || t.direction != direction || t.ctx.Err() != nil {
		return false
	}
	t.used = true
	return true
}
func (t *clipboardConnection) readReply() (clipboardControlReply, error) {
	var reply clipboardControlReply
	data, err := readBounded(t.conn)
	if err != nil || decodeExact(data, &reply) != nil || reply.Version != 1 || reply.Binding != t.binding || reply.Length < 0 || reply.Length > clipboardx.MaxTextBytes {
		return reply, clipboardx.ErrAdmission
	}
	return reply, nil
}
func (t *clipboardConnection) Write(ctx context.Context, text []byte) (clipboardx.Outcome, error) {
	if !t.claim(clipboardx.ToGuest) || ctx.Err() != nil {
		return clipboardx.Unchanged, clipboardx.ErrCancelled
	}
	if err := clipboardx.Validate(text); err != nil {
		return clipboardx.Unchanged, err
	}
	stop := context.AfterFunc(ctx, func() { t.conn.Close() })
	defer stop()
	// Until the final terminator is accepted the server cannot dispatch a write.
	var h [4]byte
	binary.BigEndian.PutUint32(h[:], uint32(len(text)))
	if _, err := io.Copy(t.conn, bytes.NewReader(h[:])); err != nil {
		return clipboardx.Unchanged, clipboardx.ErrCancelled
	}
	if _, err := io.Copy(t.conn, bytes.NewReader(text)); err != nil {
		return clipboardx.Unchanged, clipboardx.ErrCancelled
	}
	if ctx.Err() != nil {
		return clipboardx.Unchanged, clipboardx.ErrCancelled
	}
	if _, err := t.conn.Write([]byte{0}); err != nil {
		return clipboardx.Unknown, clipboardx.ErrUnknown
	}
	reply, err := t.readReply()
	if err != nil || reply.Length != 0 {
		return clipboardx.Unknown, clipboardx.ErrUnknown
	}
	if reply.Status == "ok" && reply.Outcome == clipboardx.Committed {
		return clipboardx.Committed, nil
	}
	if reply.Status == "error" && reply.Outcome == clipboardx.Unchanged {
		return clipboardx.Unchanged, clipboardx.ErrUnavailable
	}
	return clipboardx.Unknown, clipboardx.ErrUnknown
}
func (t *clipboardConnection) Read(ctx context.Context) ([]byte, error) {
	if !t.claim(clipboardx.FromGuest) || ctx.Err() != nil {
		return nil, clipboardx.ErrCancelled
	}
	stop := context.AfterFunc(ctx, func() { t.conn.Close() })
	defer stop()
	if _, err := t.conn.Write([]byte{0}); err != nil {
		return nil, clipboardx.ErrUnavailable
	}
	reply, err := t.readReply()
	if err != nil || reply.Status != "ok" || reply.Outcome != clipboardx.Unchanged {
		return nil, clipboardx.ErrUnavailable
	}
	text := make([]byte, reply.Length)
	if _, err := io.ReadFull(t.conn, text); err != nil {
		return nil, clipboardx.ErrUnavailable
	}
	var tail [1]byte
	n, err := t.conn.Read(tail[:])
	if n != 0 || !errors.Is(err, io.EOF) || ctx.Err() != nil || clipboardx.Validate(text) != nil {
		return nil, clipboardx.ErrUnavailable
	}
	return text, nil
}
