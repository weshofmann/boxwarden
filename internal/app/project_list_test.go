package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/weshofmann/boxwarden/internal/backend"
	"github.com/weshofmann/boxwarden/internal/config"
	"github.com/weshofmann/boxwarden/internal/hostidentity"
	"github.com/weshofmann/boxwarden/internal/projectx"
	"github.com/weshofmann/boxwarden/internal/session"
	"github.com/weshofmann/boxwarden/internal/supervisor"
	"github.com/weshofmann/boxwarden/internal/workspacex"
)

func snapshotProjectTree(t *testing.T, root string) map[string]string {
	t.Helper()
	result := map[string]string{}
	err := filepath.WalkDir(root, func(path string, e os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := e.Info()
		if err != nil {
			return err
		}
		data := []byte{}
		if !e.IsDir() {
			data, err = os.ReadFile(path)
			if err != nil {
				return err
			}
		}
		result[path] = fmt.Sprintf("%s:%d:%s:%x", info.Mode(), info.Size(), info.ModTime().UTC(), sha256.Sum256(data))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestProjectListSortedReadOnlyAndActions(t *testing.T) {
	prefix, d, o, b, out := projectFixture(t)
	for _, name := range []string{"zed", "alice"} {
		if err := Run(t.Context(), append(prefix, "project", "create", "--size-mib", "16", name), o); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"zed", "alice"} {
		p, _ := projectx.Load(d.StateRoot, d.ID, name)
		s, _ := session.LoadRecord(d.StateRoot, string(d.ID), name)
		b.SetObservation(backend.Observation{ObjectID: p.BackendObject, Exists: true, State: backend.ObjectStopped})
		// Listing is read-only even when lifecycle/status records were never READY.
		if s.IntendedState != session.StateStopped {
			t.Fatal("fixture unexpectedly running")
		}
	}
	out.Reset()
	before := snapshotProjectTree(t, d.StateRoot)
	if err := Run(t.Context(), append(prefix, "project", "list"), o); err != nil {
		t.Fatal(err)
	}
	after := snapshotProjectTree(t, d.StateRoot)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("list changed state tree")
	}
	text := out.String()
	if strings.Index(text, "project: alice") > strings.Index(text, "project: zed") || !strings.Contains(text, "state: stopped") || !strings.Contains(text, "next: project open alice") || !strings.Contains(text, "workspace:") || strings.Contains(text, "state: READY") {
		t.Fatalf("list lacks actionable exact state: %s", text)
	}
	for _, name := range []string{"alice", "zed"} {
		p, _ := projectx.Load(d.StateRoot, d.ID, name)
		if !strings.Contains(text, p.VolumeID) || !strings.Contains(text, p.SessionID) {
			t.Fatalf("missing exact association %s", name)
		}
	}
}

func TestProjectListEmptyAndUnavailableStorage(t *testing.T) {
	path, d := writeV2DomainFixture(t, "alpha")
	out := &bytes.Buffer{}
	o := Options{Output: out, storageCheck: syntheticStorageCheck}
	args := []string{"--config", path, "--domain", "alpha", "project", "list"}
	before := snapshotProjectTree(t, d.StateRoot)
	if err := Run(t.Context(), args, o); err != nil || !strings.Contains(out.String(), "no named projects") {
		t.Fatalf("empty: %v %s", err, out)
	}
	if !reflect.DeepEqual(before, snapshotProjectTree(t, d.StateRoot)) {
		t.Fatal("empty listing writes state")
	}
	out.Reset()
	o.BackendFactory = func(config.Config, config.Domain) (BackendDependencies, error) {
		t.Fatal("unavailable storage reached backend factory")
		return BackendDependencies{}, nil
	}
	o.storageCheck = func(hostidentity.StorageExpectation) error { return errors.New("volume unavailable") }
	if err := Run(t.Context(), args, o); err == nil || !strings.Contains(err.Error(), "volume unavailable") || out.Len() != 0 {
		t.Fatalf("storage absence hidden: %v %s", err, out)
	}
}

func TestProjectListIncompleteAndMissingDiskRemainVisible(t *testing.T) {
	prefix, d, o, _, out := projectFixture(t)
	r := projectx.Record{Version: 1, Domain: d.ID, Name: "incomplete", Base: "golden-desktop", VolumeID: "00112233-4455-6677-8899-aabbccddeeff", FilesystemUUID: "11112233-4455-6677-8899-aabbccddeeff", SizeBytes: 16 << 20}
	if err := projectx.Create(d.StateRoot, d.ID, r); err != nil {
		t.Fatal(err)
	}
	if err := Run(t.Context(), append(prefix, "project", "create", "--size-mib", "16", "demo"), o); err != nil {
		t.Fatal(err)
	}
	demo, _ := projectx.Load(d.StateRoot, d.ID, "demo")
	if err := os.Remove(filepath.Join(d.StateRoot, "volumes", demo.VolumeID+".raw")); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	before := snapshotProjectTree(t, d.StateRoot)
	if err := Run(t.Context(), append(prefix, "project", "list"), o); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, snapshotProjectTree(t, d.StateRoot)) {
		t.Fatal("incomplete list writes")
	}
	if !strings.Contains(out.String(), "state: incomplete") || !strings.Contains(out.String(), "state: unavailable") || !strings.Contains(out.String(), "session status incomplete") {
		t.Fatalf("missing incomplete/unavailable: %s", out)
	}
}

func TestProjectListRejectsForeignWorkspaceAssociationBeforeOutput(t *testing.T) {
	prefix, d, o, _, out := projectFixture(t)
	for _, name := range []string{"demo", "other"} {
		if err := Run(t.Context(), append(prefix, "project", "create", "--size-mib", "16", name), o); err != nil {
			t.Fatal(err)
		}
	}
	r, _ := projectx.Load(d.StateRoot, d.ID, "demo")
	other, _ := projectx.Load(d.StateRoot, d.ID, "other")
	path := filepath.Join(d.StateRoot, "workspaces", r.VolumeID+".json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var v map[string]any
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	attachment := v["attachment"].(map[string]any)
	attachment["session_id"] = other.SessionID
	attachment["session_name"] = "other"
	raw, _ = json.Marshal(v)
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := Run(context.Background(), append(prefix, "project", "list"), o); err == nil || !strings.Contains(err.Error(), "binding") || out.Len() != 0 {
		t.Fatalf("foreign workspace accepted: %v %s", err, out)
	}
}

func TestProjectListParserDoesNotTakeNameOrCreateOptions(t *testing.T) {
	for _, args := range [][]string{{"list", "demo"}, {"list", "--base", "current"}, {"list", "--size-mib", "64"}} {
		if _, err := parseProject(args); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
	if _, err := parseProject([]string{"list"}); err != nil {
		t.Fatal(err)
	}
}

func TestProjectListReadyRequiresFreshExactSupervisorAndDoesNotWrite(t *testing.T) {
	prefix, d, o, b, out := projectFixture(t)
	if err := Run(t.Context(), append(prefix, "project", "create", "--size-mib", "16", "demo"), o); err != nil {
		t.Fatal(err)
	}
	s, _ := session.LoadRecord(d.StateRoot, string(d.ID), "demo")
	b.SetObservation(backend.Observation{ObjectID: s.Backend.ObjectID, Exists: true, State: backend.ObjectStopped})
	var err error
	s, err = workspacex.PrepareSessionStart(t.Context(), d.StateRoot, d.ID, s, "00112233-4455-6677-8899-aabbccddeeff", b)
	if err != nil {
		t.Fatal(err)
	}
	s.IntendedState = session.StateRunning
	s.Readiness = session.ReadinessRecord{Status: session.ReadinessReady}
	if err := session.SaveRecord(d.StateRoot, d.ID, s); err != nil {
		t.Fatal(err)
	}
	b.SetObservation(backend.Observation{ObjectID: s.Backend.ObjectID, Exists: true, State: backend.ObjectRunning})
	binding := supervisor.Binding{Domain: string(d.ID), SessionID: s.ID, BackendKind: s.Backend.Kind, BackendObject: s.Backend.ObjectID, Generation: s.StartGeneration}
	reader := &statusSnapshotFake{snapshot: supervisor.Snapshot{Binding: binding, BackendRunning: true, SerialHealthy: true, PinPresent: true, CertificateCurrent: true, ProbeOK: true, ZoneMatches: true, ObservedAt: time.Now()}}
	args := append(prefix, "project", "list")
	before := snapshotProjectTree(t, d.StateRoot)
	check := func(wantReady bool) {
		t.Helper()
		out.Reset()
		if err := Run(t.Context(), args, o); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(out.String(), "state: READY") != wantReady {
			t.Fatalf("readiness=%v: %s", wantReady, out)
		}
		if !reflect.DeepEqual(before, snapshotProjectTree(t, d.StateRoot)) {
			t.Fatal("ready discovery changed state")
		}
	}
	check(false)
	o.StatusSnapshotFactory = func(config.Config, config.Domain) (StatusSnapshotReader, error) { return reader, nil }
	check(true)
	reader.snapshot.ObservedAt = time.Now().Add(-time.Minute)
	check(false)
	reader.snapshot.ObservedAt = time.Now()
	reader.snapshot.Binding.Generation = "11112233-4455-6677-8899-aabbccddeeff"
	check(false)
}
