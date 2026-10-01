//go:build n1clipboarddiagnostic && !n1candidate

package worker

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/weshofmann/boxwarden/internal/backend"
	"github.com/weshofmann/boxwarden/internal/config"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/contract"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/fixed"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/pathmeta"
	"github.com/weshofmann/boxwarden/internal/session"
)

// Lifecycle observations are supplemental. None establishes retained owner,
// Stop/Wait/reap, public lifecycle success, or authority to start/delete/adopt.
type LifecycleObservation struct {
	Version       int      `json:"version"`
	Role          string   `json:"role"`
	Name          string   `json:"name"`
	Identity      Identity `json:"identity"`
	BackendState  string   `json:"backend_state"`
	RuntimeAbsent bool     `json:"runtime_absent"`
	RecordAbsent  bool     `json:"record_absent"`
}

func createdBackend(id string) string {
	return "boxwarden-" + config.N1Domain + "-" + strings.ReplaceAll(id, "-", "")
}
func createdIdentity(id Identity) bool {
	return contract.UUID(id.Session) && id.Backend == createdBackend(id.Session)
}
func (w *Worker) Discover(ctx context.Context, id Identity) (RuntimeObservation, error) {
	if w == nil || ctx.Err() != nil || !createdIdentity(id) || id.Generation != "" {
		return RuntimeObservation{}, ErrRefused
	}
	r, e := session.LoadRecord(w.domain.StateRoot, config.N1Domain, roleName)
	if e != nil || r.ID != id.Session || r.Backend.ObjectID != id.Backend {
		return RuntimeObservation{}, ErrRefused
	}
	id.Generation = r.StartGeneration
	return w.Inspect(ctx, id)
}
func (w *Worker) InspectRunning(ctx context.Context, id Identity) (LifecycleObservation, error) {
	return w.inspectLifecycle(ctx, id, "running")
}
func (w *Worker) InspectStopped(ctx context.Context, id Identity) (LifecycleObservation, error) {
	return w.inspectLifecycle(ctx, id, "stopped")
}
func (w *Worker) InspectDeleted(ctx context.Context, id Identity) (LifecycleObservation, error) {
	return w.inspectLifecycle(ctx, id, "missing")
}
func (w *Worker) inspectLifecycle(ctx context.Context, id Identity, state string) (result LifecycleObservation, err error) {
	if w == nil || w.observer == nil || ctx.Err() != nil || !id.valid() || !createdIdentity(id) {
		return result, ErrRefused
	}
	g, e := openLifecycleNamespace(w.domain.StateRoot, id, state == "running")
	if e != nil {
		return result, ErrRefused
	}
	defer func() {
		if g.Close() != nil {
			result = LifecycleObservation{}
			err = ErrRefused
		}
	}()
	var before session.Record
	var raw []byte
	var m FileMetadata
	recordPath := w.domain.StateRoot + "/sessions/" + roleName + ".json"
	if state == "missing" {
		if !g.recordAbsent() {
			return result, ErrRefused
		}
	} else {
		raw, m, e = readLeaf(recordPath, 0600, 4096)
		if e != nil {
			return result, ErrRefused
		}
		before, e = session.LoadRecord(w.domain.StateRoot, config.N1Domain, roleName)
		if e != nil || !lifecycleRecord(before, id, state) {
			return result, ErrRefused
		}
	}
	absent := state != "running"
	for i := 0; i < 2; i++ {
		if ctx.Err() != nil || g.check() != nil {
			return result, ErrRefused
		}
		o, e := w.observer.Observe(ctx, id.Backend)
		if e != nil || o.ObjectID != id.Backend || !o.State.Valid() || (state == "missing" && (o.Exists || o.State != backend.ObjectUnknown)) || (state == "running" && (!o.Exists || o.State != backend.ObjectRunning)) || (state == "stopped" && (!o.Exists || o.State != backend.ObjectStopped)) {
			return result, ErrRefused
		}
		if absent && g.runtimeAbsent() != nil {
			return result, ErrRefused
		}
		if state == "missing" {
			if !g.recordAbsent() {
				return result, ErrRefused
			}
		} else {
			after, am, e := readLeaf(recordPath, 0600, 4096)
			if e != nil || !bytes.Equal(raw, after) || am != m {
				return result, ErrRefused
			}
			r, e := session.LoadRecord(w.domain.StateRoot, config.N1Domain, roleName)
			if e != nil || r != before || !lifecycleRecord(r, id, state) {
				return result, ErrRefused
			}
		}
		if g.check() != nil {
			return result, ErrRefused
		}
	}
	return LifecycleObservation{1, role, roleName, id, state, absent, state == "missing"}, nil
}
func lifecycleRecord(r session.Record, id Identity, state string) bool {
	if r.Domain != config.N1Domain || string(r.Name) != roleName || r.ID != id.Session || r.Backend.Kind != "tart" || r.Backend.ObjectID != id.Backend || r.Mode != session.ModeQuarantine || r.GoldenRevision != contract.BaseName || r.RecipeIntentDigest != "" {
		return false
	}
	if state == "running" {
		return r.IntendedState == session.StateRunning && r.StartGeneration == id.Generation
	}
	return state == "stopped" && r.IntendedState == session.StateStopped && r.StartGeneration == ""
}

type lifecycleNamespace struct {
	root    *os.Root
	path    string
	id      Identity
	names   []string
	pins    map[string]FileMetadata
	parents map[string]os.FileInfo
}

func openLifecycleNamespace(path string, id Identity, running bool) (g *lifecycleNamespace, err error) {
	canonical, e := filepath.EvalSymlinks(path)
	if e != nil || canonical != path {
		return nil, ErrRefused
	}
	root, e := os.OpenRoot(path)
	if e != nil {
		return nil, ErrRefused
	}
	g = &lifecycleNamespace{root: root, path: path, id: id, names: []string{".", "sessions", "runtime", "runtime/n1qualification", "runtime/n1qualification/" + id.Session}, pins: map[string]FileMetadata{}, parents: map[string]os.FileInfo{}}
	if running {
		g.names = append(g.names, "runtime/n1qualification/"+id.Session+"/"+id.Generation)
	}
	for p := filepath.Dir(path); ; p = filepath.Dir(p) {
		f, e := os.Lstat(p)
		if e != nil || !safeLifecycleParent(p, f) {
			g.Close()
			return nil, ErrRefused
		}
		g.parents[p] = f
		if p == "/" {
			break
		}
	}
	for _, n := range g.names {
		f, e := root.Lstat(n)
		if e != nil || !privateLifecycleDirectory(f) {
			g.Close()
			return nil, ErrRefused
		}
		g.pins[n] = stamp(filepath.Join(path, n), f)
	}
	if g.check() != nil {
		g.Close()
		return nil, ErrRefused
	}
	return g, nil
}
func privateLifecycleDirectory(f os.FileInfo) bool {
	if f == nil || f.Mode() != os.ModeDir|0700 {
		return false
	}
	s, ok := f.Sys().(*syscall.Stat_t)
	return ok && s.Uid == 501 && s.Nlink > 0
}
func safeLifecycleParent(p string, f os.FileInfo) bool {
	if f == nil || !f.IsDir() || f.Mode()&os.ModeSymlink != 0 || f.Mode().Perm()&0022 != 0 {
		return false
	}
	s, ok := f.Sys().(*syscall.Stat_t)
	return ok && (s.Uid == 0 || s.Uid == 501) && pathmeta.CheckQualificationAncestor(p, f, pathmeta.OSInspector{}, fixed.CheckPlatform) == nil
}
func (g *lifecycleNamespace) Close() error {
	if g == nil || g.root == nil {
		return ErrRefused
	}
	r := g.root
	g.root = nil
	return r.Close()
}
func (g *lifecycleNamespace) check() error {
	if g == nil || g.root == nil {
		return ErrRefused
	}
	canonical, e := filepath.EvalSymlinks(g.path)
	if e != nil || canonical != g.path {
		return ErrRefused
	}
	visible, e := os.Lstat(g.path)
	pinned, e2 := g.root.Stat(".")
	if e != nil || e2 != nil || stamp(g.path, visible) != g.pins["."] || stamp(g.path, pinned) != g.pins["."] {
		return ErrRefused
	}
	for p, before := range g.parents {
		after, e := os.Lstat(p)
		if e != nil || !sameParent(before, after) || !safeLifecycleParent(p, after) {
			return ErrRefused
		}
		last, e := os.Lstat(p)
		if e != nil || !sameParent(after, last) {
			return ErrRefused
		}
	}
	rootDevice := g.pins["."].Device
	for _, n := range g.names {
		absolute := filepath.Join(g.path, n)
		f, e := g.root.Lstat(n)
		if e != nil || !privateLifecycleDirectory(f) || stamp(absolute, f) != g.pins[n] || g.pins[n].Device != rootDevice || pathmeta.Check(absolute, f, pathmeta.OSInspector{}) != nil {
			return ErrRefused
		}
		last, e := g.root.Lstat(n)
		if e != nil || stamp(absolute, last) != g.pins[n] {
			return ErrRefused
		}
	}
	visible, e = os.Lstat(g.path)
	pinned, e2 = g.root.Stat(".")
	if e != nil || e2 != nil || stamp(g.path, visible) != g.pins["."] || stamp(g.path, pinned) != g.pins["."] {
		return ErrRefused
	}
	return nil
}
func (g *lifecycleNamespace) recordAbsent() bool {
	_, e := g.root.Lstat("sessions/" + roleName + ".json")
	return errors.Is(e, os.ErrNotExist)
}
func (g *lifecycleNamespace) runtimeAbsent() (err error) {
	dir := "runtime/n1qualification/" + g.id.Session
	for _, n := range []string{g.id.Generation, "." + g.id.Generation + ".cleanup", "." + g.id.Generation + ".cleanup.lock"} {
		if _, e := g.root.Lstat(dir + "/" + n); !errors.Is(e, os.ErrNotExist) {
			return ErrRefused
		}
	}
	f, e := g.root.Open(dir)
	if e != nil {
		return ErrRefused
	}
	defer func() {
		if f.Close() != nil {
			err = ErrRefused
		}
	}()
	before, e := f.Stat()
	if e != nil || stamp(filepath.Join(g.path, dir), before) != g.pins[dir] {
		return ErrRefused
	}
	names, e := f.Readdirnames(1)
	if !errors.Is(e, io.EOF) || len(names) != 0 {
		return ErrRefused
	}
	after, e := f.Stat()
	if e != nil || stamp(filepath.Join(g.path, dir), after) != g.pins[dir] {
		return ErrRefused
	}
	return nil
}
