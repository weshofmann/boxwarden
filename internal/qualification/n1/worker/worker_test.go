//go:build n1clipboarddiagnostic && !n1candidate

package worker

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/weshofmann/boxwarden/internal/backend"
	"github.com/weshofmann/boxwarden/internal/backend/fake"
	"github.com/weshofmann/boxwarden/internal/config"
	"github.com/weshofmann/boxwarden/internal/lock"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/contract"
	"github.com/weshofmann/boxwarden/internal/session"
	"github.com/weshofmann/boxwarden/internal/sshx"
)

func TestWorkerActualFreshCompositionNeverAdopts(t *testing.T) {
	root, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	b := fake.New(backend.Observation{ObjectID: contract.BaseName, Exists: true, State: backend.ObjectStopped})
	w := &Worker{domain: config.Domain{ID: "n1qualification", StateRoot: root}, observer: b, creator: b}
	c, e := w.Create(context.Background())
	if e != nil || !c.Created || c.Record.Mode != session.ModeQuarantine || c.Record.GoldenRevision != contract.BaseName || c.Record.IntendedState != session.StateStopped {
		t.Fatalf("fresh=%+v error=%v", c, e)
	}
	if _, e = w.Create(context.Background()); e == nil {
		t.Fatal("adopted existing session")
	}
	if len(b.CloneCalls()) != 1 {
		t.Fatal("created another clone")
	}
	if _, e = os.Stat(filepath.Join(root, "goldens", "current.json")); !os.IsNotExist(e) {
		t.Fatal("mutated golden current pointer")
	}
}
func TestWorkerExactIdentityRefusesStaleAndForeign(t *testing.T) {
	r := session.Record{Domain: "n1qualification", Name: session.Name(roleName), ID: "11111111-1111-4111-8111-111111111111", Mode: session.ModeQuarantine, IntendedState: session.StateRunning, Backend: session.BackendRef{Kind: "tart", ObjectID: "boxwarden-x"}, GoldenRevision: contract.BaseName, StartGeneration: "22222222-2222-4222-8222-222222222222"}
	id := Identity{Session: r.ID, Backend: r.Backend.ObjectID, Generation: r.StartGeneration}
	if admitRecord(r, id) != nil {
		t.Fatal("valid record refused")
	}
	bad := id
	bad.Generation = "33333333-3333-4333-8333-333333333333"
	if admitRecord(r, bad) == nil {
		t.Fatal("stale generation")
	}
	r.Mode = session.ModeClean
	if admitRecord(r, id) == nil {
		t.Fatal("nonquarantine record")
	}
}
func TestWorkerUsesActualTransitionThenSessionLocksAndReleases(t *testing.T) {
	root, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	w := &Worker{domain: config.Domain{ID: "n1qualification", StateRoot: root}}
	ran := false
	e = w.withLocks(context.Background(), func() error {
		ran = true
		for _, scope := range []string{"transition-n1qualification-" + roleName, "session-n1qualification-" + roleName} {
			if h, e := lock.TryAcquire(context.Background(), root, scope); e == nil {
				h.Release()
				t.Fatal("not locked", scope)
			}
		}
		return nil
	})
	if e != nil || !ran {
		t.Fatal(e)
	}
	for _, scope := range []string{"transition-n1qualification-" + roleName, "session-n1qualification-" + roleName} {
		h, e := lock.TryAcquire(context.Background(), root, scope)
		if e != nil {
			t.Fatal("not released", e)
		}
		h.Release()
	}
}

func TestWorkerPartialCreationCannotBecomeFreshRetry(t *testing.T) {
	root, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	b := fake.New(backend.Observation{ObjectID: contract.BaseName, Exists: true, State: backend.ObjectStopped})
	b.SetClonePostEffectError(errors.New("after actual clone"))
	w := &Worker{domain: config.Domain{ID: config.N1Domain, StateRoot: root}, observer: b, creator: b}
	if _, e = w.Create(context.Background()); e == nil {
		t.Fatal("partial clone claimed Created success")
	}
	b.SetClonePostEffectError(nil)
	if _, e = w.Create(context.Background()); e == nil {
		t.Fatal("adopted partially reserved role")
	}
	if len(b.CloneCalls()) != 1 {
		t.Fatal("retried mutation after reservation consumed", b.CloneCalls())
	}
}
func TestWorkerRequestExactCanonicalRoleBoundary(t *testing.T) {
	win := contract.Window{LockSHA: strings.Repeat("a", 64), ID: "11111111-1111-4111-8111-111111111111", StartedUnixNS: 1e9, ExpiresUnixNS: 1e9 + contract.WindowNS, ContinuousStartNS: 1e9, ContinuousLimitNS: 1e9 + contract.WindowNS}
	id := Identity{"22222222-2222-4222-8222-222222222222", "boxwarden-x", "33333333-3333-4333-8333-333333333333"}
	request := Request{Version: 1, Window: win, Identity: id}
	raw, e := bounded(request, contract.MaxReceiptBytes)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = parseRequest(raw, "inspect"); e != nil {
		t.Fatal("canonical control", e)
	}
	for name, bad := range map[string][]byte{"unknown": bytes.Replace(raw, []byte("\"version\":1"), []byte("\"version\":1,\"hidden\":1"), 1), "duplicate": bytes.Replace(raw, []byte("\"version\":1"), []byte("\"version\":1,\"version\":1"), 1), "whitespace": append([]byte(" "), raw...), "missing-LF": raw[:len(raw)-1], "trailing": append(append([]byte{}, raw...), raw...), "overflow": bytes.Repeat([]byte(" "), contract.MaxReceiptBytes+1)} {
		if _, e = parseRequest(bad, "inspect"); e == nil {
			t.Fatal("bad request admitted", name)
		}
	}
	request.Phase = sshx.N1GuestInitial
	raw, _ = bounded(request, contract.MaxReceiptBytes)
	if _, e = parseRequest(raw, "inspect"); e == nil {
		t.Fatal("irrelevant phase admitted")
	}
	request.Phase = ""
	request.Identity = Identity{}
	raw, _ = bounded(request, contract.MaxReceiptBytes)
	if _, e = parseRequest(raw, "create"); e != nil {
		t.Fatal("exact create", e)
	}
	if _, e = parseRequest(raw, "start"); e == nil {
		t.Fatal("public lifecycle moved into worker")
	}
	if role == "control" {
		request.Identity = id
		request.Operation = "44444444-4444-4444-8444-444444444444"
		raw, _ = bounded(request, contract.MaxReceiptBytes)
		if _, e = parseRequest(raw, "collect"); e == nil {
			t.Fatal("stock watch capability admitted")
		}
	}
}
func TestWorkerStageMissingGenericStaticEntryRefuses(t *testing.T) {
	w := &Worker{}
	if _, _, e := w.bundle(); e == nil {
		t.Fatal("missing generic source acquired fallback authority")
	}
}

func TestWorkerReadonlyLifecycleVerbAdmission(t *testing.T) {
	win := contract.Window{LockSHA: strings.Repeat("a", 64), ID: "11111111-1111-4111-8111-111111111111", StartedUnixNS: 1e9, ExpiresUnixNS: 1e9 + contract.WindowNS, ContinuousStartNS: 1e9, ContinuousLimitNS: 1e9 + contract.WindowNS}
	id := Identity{"22222222-2222-4222-8222-222222222222", "boxwarden-n1qualification-22222222222242228222222222222222", "33333333-3333-4333-8333-333333333333"}
	for _, verb := range []string{"discover", "inspect-running", "inspect-stopped", "inspect-deleted"} {
		t.Run(verb, func(t *testing.T) {
			request := Request{Version: 1, Window: win, Identity: id}
			if verb == "discover" {
				request.Identity.Generation = ""
			}
			raw, e := bounded(request, contract.MaxReceiptBytes)
			if e != nil {
				t.Fatal(e)
			}
			if _, e = parseRequest(raw, verb); e != nil {
				t.Fatal("exact readonly verb refused", e)
			}
			request.Phase = sshx.N1GuestInitial
			raw, _ = bounded(request, contract.MaxReceiptBytes)
			if _, e = parseRequest(raw, verb); e == nil {
				t.Fatal("irrelevant phase accepted")
			}
			request.Phase = ""
			if verb == "discover" {
				request.Identity.Generation = id.Generation
			} else {
				request.Identity.Generation = ""
			}
			raw, _ = bounded(request, contract.MaxReceiptBytes)
			if _, e = parseRequest(raw, verb); e == nil {
				t.Fatal("wrong generation selector admitted")
			}
		})
	}
}
