//go:build n1clipboarddiagnostic && !n1candidate

package session

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/weshofmann/boxwarden/internal/clipboarddiag"
	"github.com/weshofmann/boxwarden/internal/clipboardx"
	"github.com/weshofmann/boxwarden/internal/config"
	"github.com/weshofmann/boxwarden/internal/lock"
	"os"
	"testing"
	"time"
)

type diagnosticEndpointFixture struct {
	clipboardEndpointFake
	collect func(context.Context, clipboarddiag.Operation, clipboarddiag.Fragment) (clipboarddiag.CollectionReceipt, error)
}

func (e diagnosticEndpointFixture) Collect(ctx context.Context, o clipboarddiag.Operation, f clipboarddiag.Fragment) (clipboarddiag.CollectionReceipt, error) {
	return e.collect(ctx, o, f)
}
func TestDiagnosticServiceFixedFixtureLocksReadPrivacyAndCollectionNoTransfer(t *testing.T) {
	sum := sha256.Sum256([]byte(ClipboardDiagnosticFixture))
	if len(ClipboardDiagnosticFixture) != 31 || hex.EncodeToString(sum[:]) != ClipboardDiagnosticFixtureSHA256 {
		t.Fatal("fixture catalogue changed")
	}
	root, _ := actionAttemptFixture(t)
	record, err := LoadRecord(root, "work", "dev")
	if err != nil {
		t.Fatal(err)
	}
	record.Domain = "n1qualification"
	if SaveRecord(root, record.Domain, record) != nil {
		t.Fatal("synthetic domain record")
	}
	op := clipboarddiag.Operation{Version: 1, OperationID: "00000000-0000-4000-8000-000000000001", Direction: "write", Domain: string(record.Domain), SessionID: record.ID, BackendKind: record.Backend.Kind, BackendObject: record.Backend.ObjectID, Generation: record.StartGeneration, ExpiresAt: time.Now().UTC().Add(time.Second)}
	calls := 0
	transfer := &clipboardTransferFake{read: []byte("unexpected-private-synthetic")}
	requireHeld := func() {
		for _, name := range []string{"transition-n1qualification-dev", "session-n1qualification-dev"} {
			h, e := lock.TryAcquire(t.Context(), root, name)
			if h != nil {
				h.Release()
			}
			if !errors.Is(e, lock.ErrBusy) {
				t.Fatal("actual lifecycle lock absent")
			}
		}
	}
	endpoint := diagnosticEndpointFixture{clipboardEndpointFake: clipboardEndpointFake{begin: func(context.Context, clipboardx.Target, clipboardx.Direction) (clipboardx.Transfer, error) {
		calls++
		requireHeld()
		return transfer, nil
	}}, collect: func(_ context.Context, o clipboarddiag.Operation, f clipboarddiag.Fragment) (clipboarddiag.CollectionReceipt, error) {
		requireHeld()
		return clipboarddiag.CollectionReceipt{Version: 1, Binding: o, Fragments: []clipboarddiag.Fragment{f}}, nil
	}}
	service, e := NewClipboardDiagnosticService(config.Domain{ID: record.Domain, StateRoot: root}, endpoint)
	if e != nil {
		t.Fatal(e)
	}
	receipt, e := service.Invoke(t.Context(), "dev", op)
	if e != nil || receipt.Outcome != "committed" || !bytes.Equal(transfer.write, []byte(ClipboardDiagnosticFixture)) || calls != 1 {
		t.Fatal("fixed real-service invocation failed", e)
	}
	refused := op
	refused.SessionID = "00000000-0000-4000-8000-000000000009"
	rejected, refusalErr := service.Invoke(t.Context(), "dev", refused)
	if refusalErr != nil || rejected.Outcome != "unchanged" || rejected.Synthetic.Length != 0 || rejected.Synthetic.Equal || calls != 1 {
		t.Fatal("pre-admission receipt invented captured bytes", refusalErr, rejected.Synthetic)
	}
	op.Direction = "read"
	op.OperationID = "00000000-0000-4000-8000-000000000004"
	receipt, e = service.Invoke(t.Context(), "dev", op)
	if e != nil || receipt.Synthetic.Equal || receipt.Synthetic.Length != len(transfer.read) {
		t.Fatal("read equality incorrectly promoted", e)
	}
	raw, _ := json.Marshal(receipt)
	if bytes.Contains(raw, transfer.read) || bytes.Contains(raw, []byte("payload")) {
		t.Fatal("read payload disclosed")
	}
	_, e = service.Collect(t.Context(), "dev", op, receipt.Fragments[0])
	if e != nil || calls != 2 {
		t.Fatal("collection retrieved clipboard/replayed transfer", e)
	}
}

type diagnosticUnknownTransfer struct{ clipboardTransferFake }

func (t *diagnosticUnknownTransfer) Write(_ context.Context, text []byte) (clipboardx.Outcome, error) {
	t.write = append([]byte{}, text...)
	return clipboardx.Unknown, clipboardx.ErrUnknown
}
func TestDiagnosticUnknownInvocationRetainsCompleteMetadataWithoutPromotion(t *testing.T) {
	root, _ := actionAttemptFixture(t)
	record, err := LoadRecord(root, "work", "dev")
	if err != nil {
		t.Fatal(err)
	}
	record.Domain = "n1qualification"
	if SaveRecord(root, record.Domain, record) != nil {
		t.Fatal("synthetic domain record")
	}
	op := clipboarddiag.Operation{Version: 1, OperationID: "00000000-0000-4000-8000-000000000001", Direction: "write", Domain: string(record.Domain), SessionID: record.ID, BackendKind: record.Backend.Kind, BackendObject: record.Backend.ObjectID, Generation: record.StartGeneration, ExpiresAt: time.Now().UTC().Add(5 * time.Second)}
	raw, err := os.ReadFile("../clipboarddiag/testdata/task2-r1-receipts.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []clipboarddiag.OverlayReceipt
	if json.Unmarshal(raw, &fixtures) != nil {
		t.Fatal("frozen metadata fixture")
	}
	overlay := fixtures[1]
	overlay.Binding = op
	overlay.HeaderDigest = clipboarddiag.HeaderDigest(op)
	overlay.Closure.HeaderDigest = overlay.HeaderDigest
	for i := range overlay.Records {
		overlay.Records[i].HeaderDigest = overlay.HeaderDigest
	}
	if overlay.Validate(op) != nil {
		t.Fatal("native progression fixture")
	}
	transfer := &diagnosticUnknownTransfer{}
	calls, collections := 0, 0
	endpoint := diagnosticEndpointFixture{clipboardEndpointFake: clipboardEndpointFake{begin: func(context.Context, clipboardx.Target, clipboardx.Direction) (clipboardx.Transfer, error) {
		calls++
		return transfer, nil
	}}, collect: func(_ context.Context, o clipboarddiag.Operation, cli clipboarddiag.Fragment) (clipboarddiag.CollectionReceipt, error) {
		collections++
		host, _ := clipboarddiag.NewRecorder(o, "supervisor", time.Now)
		hctx, _ := clipboarddiag.WithContext(t.Context(), o, host)
		clipboarddiag.Record(clipboarddiag.WithSource(hctx, "ssh"), "ssh_ack_malformed", "unknown")
		clipboarddiag.Record(hctx, "control_final_error_frame", "ok")
		guest, _ := clipboarddiag.NewRecorder(o, "guest", time.Now)
		gctx, _ := clipboarddiag.WithContext(t.Context(), o, guest)
		clipboarddiag.Record(clipboarddiag.WithSource(gctx, "helper"), "helper_namespace", "ok")
		clipboarddiag.Record(gctx, "guest_complete", "ok")
		clipboarddiag.Record(clipboarddiag.WithSource(gctx, "helper"), "helper_response", "ok")
		return clipboarddiag.CollectionReceipt{Version: 1, Binding: o, Complete: true, Fragments: []clipboarddiag.Fragment{host.Fragment(), cli, guest.Fragment()}, Overlay: &overlay}, nil
	}}
	service, err := NewClipboardDiagnosticService(config.Domain{ID: record.Domain, StateRoot: root}, endpoint)
	if err != nil {
		t.Fatal(err)
	}
	invocation, err := service.Invoke(t.Context(), "dev", op)
	if err != nil || invocation.Outcome != "unknown" || invocation.Synthetic.Equal || invocation.Synthetic.Length != 31 || calls != 1 {
		t.Fatal("unknown source outcome changed", err)
	}
	collection, err := service.Collect(t.Context(), "dev", op, invocation.Fragments[0])
	if err != nil || !collection.Complete || collection.Validate() != nil || calls != 1 || collections != 1 || invocation.Outcome != "unknown" || invocation.Synthetic.Equal {
		t.Fatal("metadata completeness promoted outcome/equality or retried transfer", err)
	}
}
