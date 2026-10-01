//go:build n1clipboarddiagnostic && !n1candidate

package supervisor

import (
	"context"
	"encoding/json"
	"github.com/weshofmann/boxwarden/internal/clipboarddiag"
	"github.com/weshofmann/boxwarden/internal/clipboardx"
	"net"
	"path/filepath"
	"sync"
	"time"
)

func clipboardStage(ctx context.Context, stage, status string) {
	clipboarddiag.Record(clipboarddiag.WithSource(ctx, "supervisor"), stage, status)
}
func clipboardControlLoss(ctx context.Context) {
	_, r, _ := clipboarddiag.Get(ctx)
	if r != nil {
		r.Invalidate()
	}
}
func operationMatchesBinding(o clipboarddiag.Operation, b Binding) bool {
	return o.Domain == b.Domain && o.SessionID == b.SessionID && o.BackendKind == b.BackendKind && o.BackendObject == b.BackendObject && o.Generation == b.Generation
}

type diagnosticEntry struct {
	operation           clipboarddiag.Operation
	recorder            *clipboarddiag.Recorder
	outcome             clipboardx.Outcome
	finished, collected bool
	publication         *clipboarddiag.PublicationWitness
	publicationSeen     bool
}
type diagnosticScope struct {
	mu                    sync.Mutex
	binding               Binding
	directions            map[string]*diagnosticEntry
	generationPublication string
}
type diagnosticScopeKey struct{}

func clipboardDiagnosticScope(ctx context.Context, b Binding) context.Context {
	return context.WithValue(ctx, diagnosticScopeKey{}, &diagnosticScope{binding: b, directions: map[string]*diagnosticEntry{}})
}

type diagnosticInvokeRequest struct {
	Version   int                     `json:"version"`
	Action    string                  `json:"action"`
	Binding   Binding                 `json:"binding"`
	Operation clipboarddiag.Operation `json:"operation"`
}
type diagnosticCollectRequest struct {
	Version   int                     `json:"version"`
	Action    string                  `json:"action"`
	Binding   Binding                 `json:"binding"`
	Operation clipboarddiag.Operation `json:"operation"`
	CLI       clipboarddiag.Fragment  `json:"cli"`
}

func handleClipboardDiagnosticControl(parent context.Context, c net.Conn, b Binding, owner RuntimeOwner, raw []byte, accepted time.Time) bool {
	var kind struct {
		Action string `json:"action"`
	}
	if json.Unmarshal(raw, &kind) != nil || (kind.Action != "n1_clipboard_invoke" && kind.Action != "n1_clipboard_collect") {
		return false
	}
	scope, _ := parent.Value(diagnosticScopeKey{}).(*diagnosticScope)
	if scope == nil || scope.binding != b {
		return true
	}
	if kind.Action == "n1_clipboard_invoke" {
		var request diagnosticInvokeRequest
		if clipboarddiag.StrictDecode(raw, &request, maxControlBytes) != nil || request.Version != 1 || request.Action != kind.Action || request.Binding != b || request.Operation.Validate(true) != nil || !operationMatchesBinding(request.Operation, b) {
			return true
		}
		op := request.Operation
		r, err := clipboarddiag.NewRecorder(op, "supervisor", time.Now)
		if err != nil {
			return true
		}
		scope.mu.Lock()
		if scope.directions[op.Direction] != nil {
			scope.mu.Unlock()
			return true
		}
		entry := &diagnosticEntry{operation: op, recorder: r, outcome: clipboardx.Unchanged}
		scope.directions[op.Direction] = entry
		scope.mu.Unlock()
		ctx, err := clipboarddiag.WithContext(parent, op, r)
		if err != nil {
			return true
		}
		ctx = context.WithValue(ctx, diagnosticEntryKey{}, entry)
		ctx = clipboarddiag.WithPublicationObserver(ctx, func(w clipboarddiag.PublicationWitness) {
			scope.mu.Lock()
			defer scope.mu.Unlock()
			if entry.publicationSeen {
				entry.publication = nil
				return
			}
			entry.publicationSeen = true
			raw, e := clipboarddiag.EncodePublication(w)
			if e != nil {
				return
			}
			admitted, e := clipboarddiag.DecodePublication(raw, entry.operation, "invoke")
			if e != nil {
				return
			}
			if admitted.Created {
				if scope.generationPublication != "" {
					return
				}
				scope.generationPublication = admitted.Generation
			} else if scope.generationPublication != admitted.Generation {
				return
			}
			entry.publication = &admitted
		})
		action := "clipboard_read"
		if op.Direction == "write" {
			action = "clipboard_write"
		}
		handleClipboardControl(ctx, c, b, owner, controlRequest{Version: 1, Action: action, Binding: b, ExpiresAt: op.ExpiresAt}, accepted)
		scope.mu.Lock()
		entry.finished = true
		scope.mu.Unlock()
		return true
	}
	var request diagnosticCollectRequest
	if clipboarddiag.StrictDecode(raw, &request, maxControlBytes) != nil || request.Version != 1 || request.Action != kind.Action || request.Binding != b || request.Operation.Validate(false) != nil || !operationMatchesBinding(request.Operation, b) || request.CLI.Validate() != nil || request.CLI.Binding != request.Operation || request.CLI.Origin != "cli" {
		return true
	}
	op := request.Operation
	scope.mu.Lock()
	entry := scope.directions[op.Direction]
	if entry == nil || entry.operation != op || !entry.finished || entry.collected {
		scope.mu.Unlock()
		return true
	}
	entry.collected = true
	host := entry.recorder.Fragment()
	scope.mu.Unlock()
	ctx, cancel := context.WithTimeout(parent, controlIOTimeout)
	defer cancel()
	deadline, _ := ctx.Deadline()
	if c.SetDeadline(deadline) != nil {
		return true
	}
	fragments, complete := clipboarddiag.MergeHost(op, host, request.CLI)
	receipt := clipboarddiag.CollectionReceipt{Version: 1, Binding: op, Fragments: fragments}
	before := owner.Snapshot(ctx)
	if before.Binding == b && snapshotReady(before) && ctx.Err() == nil {
		if collector, ok := owner.(interface {
			CollectClipboardDiagnostic(context.Context, clipboarddiag.Operation) (clipboarddiag.GuestCollection, error)
		}); ok {
			guest, err := collector.CollectClipboardDiagnostic(ctx, op)
			if err == nil && guest.Validate(op) == nil && admittedPublication(scope, entry, guest) {
				if guest.Bootstrap != nil {
					receipt.Fragments = append(receipt.Fragments, *guest.Bootstrap)
				}
				receipt.Overlay = guest.Overlay
				complete = complete && guest.Complete
			} else {
				complete = false
			}
		} else {
			complete = false
		}
	} else {
		complete = false
	}
	after := owner.Snapshot(ctx)
	if after.Binding != b || !snapshotReady(after) || ctx.Err() != nil {
		complete = false
	}
	receipt.Complete = complete
	if receipt.Validate() != nil {
		receipt.Complete = false
		receipt.Fragments = fragments
		receipt.Overlay = nil
	}
	data, err := json.Marshal(receipt)
	if err == nil && len(data)+1 <= clipboarddiag.MaxCollectionBytes {
		_ = writeClipboardDiagnosticFrame(c, data)
	}
	return true
}

type diagnosticEntryKey struct{}

func clipboardDiagnosticOutcome(ctx context.Context, outcome clipboardx.Outcome) {
	if entry, ok := ctx.Value(diagnosticEntryKey{}).(*diagnosticEntry); ok {
		entry.outcome = outcome
	}
}
func clipboardBeginRequest(ctx context.Context, b Binding, action string, expiry time.Time) ([]byte, error) {
	if o, _, ok := clipboarddiag.Get(ctx); ok {
		if !clipboarddiag.Available(ctx) || o.Validate(true) != nil || !operationMatchesBinding(o, b) || !o.ExpiresAt.Equal(expiry) || (o.Direction == "write") != (action == "clipboard_write") {
			return nil, clipboardx.ErrAdmission
		}
		return json.Marshal(diagnosticInvokeRequest{1, "n1_clipboard_invoke", b, o})
	}
	return json.Marshal(controlRequest{Version: 1, Binding: b, Action: action, ExpiresAt: expiry.UTC()})
}

// ExactClipboardDiagnosticController reuses the ordinary generation ownership,
// handshake and transfer primitives. It adds no listener or locator override.
type ExactClipboardDiagnosticController struct{ ordinary *ExactClipboardController }

func NewExactClipboardDiagnosticController(root string) (*ExactClipboardDiagnosticController, error) {
	ordinary, err := NewExactClipboardController(root)
	if err != nil {
		return nil, err
	}
	return &ExactClipboardDiagnosticController{ordinary}, nil
}
func (c *ExactClipboardDiagnosticController) Begin(ctx context.Context, target clipboardx.Target, direction clipboardx.Direction) (clipboardx.Transfer, error) {
	o, _, ok := clipboarddiag.Get(ctx)
	if c == nil || !ok || o.Validate(true) != nil || !clipboarddiag.Available(ctx) || !operationMatchesBinding(o, Binding{target.Domain, target.SessionID, target.BackendKind, target.BackendObject, target.Generation}) {
		return nil, clipboardx.ErrAdmission
	}
	return c.ordinary.Begin(ctx, target, direction)
}
func (c *ExactClipboardDiagnosticController) Collect(ctx context.Context, o clipboarddiag.Operation, cli clipboarddiag.Fragment) (clipboarddiag.CollectionReceipt, error) {
	if c == nil || c.ordinary == nil || o.Validate(false) != nil || cli.Validate() != nil || cli.Binding != o || cli.Origin != "cli" {
		return clipboarddiag.CollectionReceipt{}, clipboarddiag.ErrMetadata
	}
	b := Binding{o.Domain, o.SessionID, o.BackendKind, o.BackendObject, o.Generation}
	dir, err := exactRuntimeDirectory(c.ordinary.runtimeRoot, b)
	if err != nil {
		return clipboarddiag.CollectionReceipt{}, clipboarddiag.ErrMetadata
	}
	request, err := readLaunchRequest(filepath.Join(dir, requestName))
	if err != nil || request.Binding != b || validateGenerationEntry(dir, socketName) != nil {
		return clipboarddiag.CollectionReceipt{}, clipboarddiag.ErrMetadata
	}
	state, err := classifyExactGeneration(request)
	if err != nil || state != exactGenerationLive {
		return clipboarddiag.CollectionReceipt{}, clipboarddiag.ErrMetadata
	}
	ctx, cancel := context.WithTimeout(ctx, controlIOTimeout)
	defer cancel()
	conn, err := dialControl(ctx, filepath.Join(dir, socketName))
	if err != nil {
		return clipboarddiag.CollectionReceipt{}, clipboarddiag.ErrMetadata
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	deadline, _ := ctx.Deadline()
	if conn.SetDeadline(deadline) != nil {
		return clipboarddiag.CollectionReceipt{}, clipboarddiag.ErrMetadata
	}
	raw, _ := json.Marshal(diagnosticCollectRequest{1, "n1_clipboard_collect", b, o, cli})
	if writeFrame(conn, raw) != nil {
		return clipboarddiag.CollectionReceipt{}, clipboarddiag.ErrMetadata
	}
	data, err := readClipboardDiagnosticFrame(conn)
	if err != nil {
		return clipboarddiag.CollectionReceipt{}, clipboarddiag.ErrMetadata
	}
	var receipt clipboarddiag.CollectionReceipt
	if clipboarddiag.StrictDecode(data, &receipt, clipboarddiag.MaxCollectionBytes) != nil || receipt.Binding != o || receipt.Validate() != nil {
		return clipboarddiag.CollectionReceipt{}, clipboarddiag.ErrMetadata
	}
	return receipt, nil
}

func admittedPublication(scope *diagnosticScope, entry *diagnosticEntry, guest clipboarddiag.GuestCollection) bool {
	scope.mu.Lock()
	defer scope.mu.Unlock()
	return entry.publication != nil && scope.generationPublication == clipboarddiag.GenerationDigest(entry.operation) && entry.publication.MatchesGuest(entry.operation, guest)
}
