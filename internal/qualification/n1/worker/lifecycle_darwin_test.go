//go:build darwin && cgo && n1clipboarddiagnostic && !n1candidate

package worker

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/weshofmann/boxwarden/internal/backend"
	"github.com/weshofmann/boxwarden/internal/backend/fake"
	"github.com/weshofmann/boxwarden/internal/config"
	"github.com/weshofmann/boxwarden/internal/networkdiag"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/contract"
	"github.com/weshofmann/boxwarden/internal/session"
	"github.com/weshofmann/boxwarden/internal/supervisor"
)

func lifecycleFixture(t *testing.T) (*Worker, Identity, *fake.Backend, string) {
	t.Helper()
	root, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	b := fake.New(backend.Observation{ObjectID: contract.BaseName, Exists: true, State: backend.ObjectStopped})
	w := &Worker{domain: config.Domain{ID: config.N1Domain, StateRoot: root}, observer: b, creator: b}
	c, e := w.Create(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	id := Identity{c.Record.ID, c.Record.Backend.ObjectID, "22222222-2222-4222-8222-222222222222"}
	parent := root + "/runtime/n1qualification/" + id.Session
	if e = os.MkdirAll(parent, 0700); e != nil {
		t.Fatal(e)
	}
	return w, id, b, parent
}
func TestWorkerReadonlyLifecycleStoppedThenDeleted(t *testing.T) {
	w, id, b, _ := lifecycleFixture(t)
	stopped, e := w.InspectStopped(context.Background(), id)
	if e != nil || stopped.BackendState != "stopped" || !stopped.RuntimeAbsent || stopped.RecordAbsent || stopped.Identity != id {
		t.Fatal("supplemental stopped observation", stopped, e)
	}
	if e = os.Remove(w.domain.StateRoot + "/sessions/" + roleName + ".json"); e != nil {
		t.Fatal(e)
	}
	b.SetObservation(backend.Observation{ObjectID: id.Backend, State: backend.ObjectUnknown})
	gone, e := w.InspectDeleted(context.Background(), id)
	if e != nil || gone.BackendState != "missing" || !gone.RecordAbsent || !gone.RuntimeAbsent || gone.Identity != id {
		t.Fatal("supplemental deleted observation", gone, e)
	}
	if len(b.StartCalls()) != 0 || len(b.DeleteCalls()) != 0 || len(b.CloneCalls()) != 1 {
		t.Fatal("readonly probes dispatched lifecycle effects")
	}
	bad := id
	bad.Backend = "arbitrary-object"
	if _, e = w.InspectDeleted(context.Background(), bad); e == nil {
		t.Fatal("record absence authorized arbitrary backend selector")
	}
}
func TestWorkerReadonlyLifecycleResidueAndMissingParentsRefuse(t *testing.T) {
	for _, kind := range []string{"generation", "cleanup", "cleanup-lock", "foreign-entry", "missing-session-parent", "missing-domain-parent", "registry-symlink"} {
		t.Run(kind, func(t *testing.T) {
			w, id, _, parent := lifecycleFixture(t)
			switch kind {
			case "generation":
				if e := os.Mkdir(parent+"/"+id.Generation, 0700); e != nil {
					t.Fatal(e)
				}
			case "cleanup":
				if e := os.Mkdir(parent+"/."+id.Generation+".cleanup", 0700); e != nil {
					t.Fatal(e)
				}
			case "cleanup-lock":
				if e := os.WriteFile(parent+"/."+id.Generation+".cleanup.lock", nil, 0600); e != nil {
					t.Fatal(e)
				}
			case "foreign-entry":
				if e := os.WriteFile(parent+"/.foreign", nil, 0600); e != nil {
					t.Fatal(e)
				}
			case "missing-session-parent":
				if e := os.Remove(parent); e != nil {
					t.Fatal(e)
				}
			case "missing-domain-parent":
				if e := os.Remove(parent); e != nil {
					t.Fatal(e)
				}
				if e := os.Remove(filepath.Dir(parent)); e != nil {
					t.Fatal(e)
				}
			case "registry-symlink":
				p := w.domain.StateRoot + "/sessions"
				if e := os.Rename(p, p+"-foreign"); e != nil {
					t.Fatal(e)
				}
				if e := os.Symlink(p+"-foreign", p); e != nil {
					t.Fatal(e)
				}
			}
			if _, e := w.InspectStopped(context.Background(), id); e == nil {
				t.Fatal("unsafe absence accepted", kind)
			}
		})
	}
}

type lifecycleObserver struct {
	base    backend.Observer
	onFirst func()
	calls   int
}

func (o *lifecycleObserver) Observe(ctx context.Context, id string) (backend.Observation, error) {
	o.calls++
	v, e := o.base.Observe(ctx, id)
	if o.calls == 1 && o.onFirst != nil {
		o.onFirst()
	}
	return v, e
}
func TestWorkerReadonlyLifecycleNamespaceReplacementRefuses(t *testing.T) {
	for _, kind := range []string{"replace-session-parent", "missing-session-parent", "record-change", "backend-change"} {
		t.Run(kind, func(t *testing.T) {
			w, id, b, parent := lifecycleFixture(t)
			changed := false
			hook := &lifecycleObserver{base: b, onFirst: func() {
				changed = true
				switch kind {
				case "replace-session-parent":
					if e := os.Rename(parent, parent+"-old"); e != nil {
						t.Fatal(e)
					}
					if e := os.Mkdir(parent, 0700); e != nil {
						t.Fatal(e)
					}
				case "missing-session-parent":
					if e := os.Remove(parent); e != nil {
						t.Fatal(e)
					}
				case "record-change":
					r, e := session.LoadRecord(w.domain.StateRoot, config.N1Domain, roleName)
					if e != nil {
						t.Fatal(e)
					}
					r.Readiness.Diagnostic = "changed"
					if e = session.SaveRecord(w.domain.StateRoot, config.N1Domain, r); e != nil {
						t.Fatal(e)
					}
				case "backend-change":
					b.SetObservation(backend.Observation{ObjectID: id.Backend, Exists: true, State: backend.ObjectRunning})
				}
			}}
			w.observer = hook
			if _, e := w.InspectStopped(context.Background(), id); e == nil {
				t.Fatal("in-flight replacement accepted")
			}
			if !changed {
				t.Fatal("mutation not reached")
			}
		})
	}
}

type forbiddenLifecycleReader struct{ t *testing.T }

func (r forbiddenLifecycleReader) Snapshot(context.Context, supervisor.Binding) (supervisor.Snapshot, error) {
	r.t.Fatal("containment observation invoked guest-health Snapshot")
	return supervisor.Snapshot{}, ErrRefused
}
func (r forbiddenLifecycleReader) InspectDiagnosticNetwork(context.Context, supervisor.Binding) (networkdiag.Inspection, error) {
	r.t.Fatal("containment observation invoked network RPC")
	return networkdiag.Inspection{}, ErrRefused
}
func TestWorkerReadonlyRunningSupportsPostCollectNonReady(t *testing.T) {
	w, id, b, parent := lifecycleFixture(t)
	r, e := session.LoadRecord(w.domain.StateRoot, config.N1Domain, roleName)
	if e != nil {
		t.Fatal(e)
	}
	r.StartGeneration = id.Generation
	r.IntendedState = session.StateRunning
	r.Readiness = session.ReadinessRecord{Status: session.ReadinessDrift, Diagnostic: "post-Collect health unproven"}
	if e = session.SaveRecord(w.domain.StateRoot, config.N1Domain, r); e != nil {
		t.Fatal(e)
	}
	if e = os.Mkdir(parent+"/"+id.Generation, 0700); e != nil {
		t.Fatal(e)
	}
	b.SetObservation(backend.Observation{ObjectID: id.Backend, Exists: true, State: backend.ObjectRunning})
	w.reader = forbiddenLifecycleReader{t}
	w.guest = nil
	got, e := w.InspectRunning(context.Background(), id)
	if e != nil || got.BackendState != "running" || got.RuntimeAbsent || got.RecordAbsent || got.Identity != id {
		t.Fatal("postCollect containment observation", got, e)
	}
	bad := id
	bad.Generation = "33333333-3333-4333-8333-333333333333"
	if _, e = w.InspectRunning(context.Background(), bad); e == nil {
		t.Fatal("stale generation admitted")
	}
}
