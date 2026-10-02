package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/weshofmann/boxwarden/internal/backend"
	"github.com/weshofmann/boxwarden/internal/config"
	"github.com/weshofmann/boxwarden/internal/projectx"
	"github.com/weshofmann/boxwarden/internal/session"
	"github.com/weshofmann/boxwarden/internal/workspacex"
)

func TestProjectRebuildSameBaseRetainsWorkAndRebindsOpenExport(t *testing.T) {
	prefix, d, o, b, _ := projectFixture(t)
	if err := Run(t.Context(), append(prefix, "project", "create", "demo"), o); err != nil {
		t.Fatal(err)
	}
	before, _ := projectx.Load(d.StateRoot, d.ID, "demo")
	before.ImportID = "00112233-4455-6677-8899-aabbccddeeff"
	before.ImportSource = "/private/source"
	before.Imported = true
	if err := projectx.Save(d.StateRoot, d.ID, before); err != nil {
		t.Fatal(err)
	}
	workspaceBefore, _ := workspacex.LoadRecord(d.StateRoot, d.ID, before.VolumeID)
	operation := "77772233-4455-6677-8899-aabbccddeeff"
	j := session.RebuildJournal{Version: 1, Domain: d.ID, SessionName: before.Name, SessionID: before.SessionID, OperationID: operation, Phase: session.RebuildCloned, OldBackend: before.BackendObject, OldRevision: before.Base, CandidateBackend: "boxwarden-alpha-77772233445566778899aabbccddeeff", CandidateRevision: before.Base}
	prepared, completed := 0, 0
	o.AlphaRebuildPrepare = func(_ context.Context, _ config.Config, actual config.Domain, _ string, name, base string) (session.RebuildJournal, error) {
		prepared++
		if actual != d || name != before.Name || base != before.Base {
			t.Fatal("wrong replacement target")
		}
		return j, nil
	}
	o.AlphaRebuildCandidate = func(_ context.Context, _ config.Config, _ config.Domain, _ string, expected session.RebuildJournal) (session.Record, error) {
		name, base := expected.SessionName, expected.CandidateRevision
		if expected.CandidateBackend != j.CandidateBackend || expected.OperationID != j.OperationID {
			t.Fatal("wrong frozen candidate")
		}
		completed++
		intent, err := projectx.LoadReplacement(d.StateRoot, d.ID, name)
		if err != nil || intent.BackendObject != j.CandidateBackend {
			t.Fatal("candidate execution without exact persisted project receipt")
		}
		s, err := session.LoadRecord(d.StateRoot, string(d.ID), name)
		if err != nil {
			t.Fatal(err)
		}
		s.Backend.ObjectID = j.CandidateBackend
		s.GoldenRevision = base
		if err := session.SaveRecord(d.StateRoot, d.ID, s); err != nil {
			t.Fatal(err)
		}
		b.SetObservation(backend.Observation{ObjectID: j.CandidateBackend, Exists: true, State: backend.ObjectStopped})
		return s, nil
	}
	if err := Run(t.Context(), append(prefix, "project", "rebuild", "--base", "current", "demo"), o); err != nil {
		t.Fatal(err)
	}
	after, _ := projectx.Load(d.StateRoot, d.ID, "demo")
	want := before
	want.BackendObject = j.CandidateBackend
	if after != want {
		t.Fatalf("work/name/import/session identity changed: %+v", after)
	}
	workspaceAfter, _ := workspacex.LoadRecord(d.StateRoot, d.ID, before.VolumeID)
	if !reflect.DeepEqual(workspaceAfter, workspaceBefore) {
		t.Fatal("rebuild rewrote workspace association")
	}
	if prepared != 1 || completed != 1 || len(b.CloneCalls()) != 1 {
		t.Fatal("project wrapper created/formatted extra resources")
	}
	if err := Run(t.Context(), append(prefix, "project", "open", "demo"), o); err != nil {
		t.Fatal(err)
	}
	o.AlphaExport = func(_ context.Context, _ config.Domain, in AlphaExportInput, _ backend.Observer) (workspacex.ExportJournal, string, error) {
		if in.VolumeID != before.VolumeID || in.Selected[0] != "boxwarden-import-"+before.ImportID {
			t.Fatal("export lost original workspace/selection")
		}
		return workspacex.ExportJournal{}, "", errors.New("new binding reached exporter")
	}
	err := Run(t.Context(), append(prefix, "project", "export", "--destination", t.TempDir()+"/new", "demo"), o)
	if err == nil || !strings.Contains(err.Error(), "new binding reached exporter") {
		t.Fatalf("export rejected replacement binding: %v", err)
	}
}

func TestProjectRebuildRefusesRunningBeforePreparing(t *testing.T) {
	prefix, d, o, b, _ := projectFixture(t)
	if err := Run(t.Context(), append(prefix, "project", "create", "demo"), o); err != nil {
		t.Fatal(err)
	}
	r, _ := projectx.Load(d.StateRoot, d.ID, "demo")
	b.SetObservation(backend.Observation{ObjectID: r.BackendObject, Exists: true, State: backend.ObjectRunning})
	o.AlphaRebuildPrepare = func(context.Context, config.Config, config.Domain, string, string, string) (session.RebuildJournal, error) {
		t.Fatal("running project reached candidate clone")
		return session.RebuildJournal{}, nil
	}
	o.AlphaRebuildCandidate = func(context.Context, config.Config, config.Domain, string, session.RebuildJournal) (session.Record, error) {
		t.Fatal("running project rebuilt")
		return session.Record{}, nil
	}
	err := Run(t.Context(), append(prefix, "project", "rebuild", "demo"), o)
	if err == nil || !strings.Contains(err.Error(), "stopped") {
		t.Fatalf("running replacement: %v", err)
	}
}

func TestProjectRebuildRetryPrefersNewPreparationOverCompletedHistory(t *testing.T) {
	prefix, d, o, b, _ := projectFixture(t)
	if err := Run(t.Context(), append(prefix, "project", "create", "demo"), o); err != nil {
		t.Fatal(err)
	}
	first, _ := projectx.Load(d.StateRoot, d.ID, "demo")
	makeJournal := func(before projectx.Record, id string) session.RebuildJournal {
		return session.RebuildJournal{Version: 1, Domain: d.ID, SessionName: before.Name, SessionID: before.SessionID, OperationID: id, Phase: session.RebuildCloned, OldBackend: before.BackendObject, OldRevision: before.Base, CandidateBackend: "boxwarden-alpha-" + strings.ReplaceAll(id, "-", ""), CandidateRevision: before.Base}
	}
	oldJournal := makeJournal(first, "77772233-4455-6677-8899-aabbccddeeff")
	oldIntent, err := projectx.BeginReplacement(d.StateRoot, d.ID, first, oldJournal)
	if err != nil {
		t.Fatal(err)
	}
	current, err := projectx.CompleteReplacement(d.StateRoot, d.ID, oldIntent)
	if err != nil {
		t.Fatal(err)
	}
	s, _ := session.LoadRecord(d.StateRoot, string(d.ID), "demo")
	s.Backend.ObjectID = current.BackendObject
	if err := session.SaveRecord(d.StateRoot, d.ID, s); err != nil {
		t.Fatal(err)
	}
	b.SetObservation(backend.Observation{ObjectID: current.BackendObject, Exists: true, State: backend.ObjectStopped})
	newer := makeJournal(current, "88882233-4455-6677-8899-aabbccddeeff")
	dir := filepath.Join(d.StateRoot, "rebuilds")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(newer)
	if err := os.WriteFile(filepath.Join(dir, "demo.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	prepared, completed := 0, 0
	o.AlphaRebuildPrepare = func(context.Context, config.Config, config.Domain, string, string, string) (session.RebuildJournal, error) {
		prepared++
		return newer, nil
	}
	o.AlphaRebuildCandidate = func(_ context.Context, _ config.Config, _ config.Domain, _ string, expected session.RebuildJournal) (session.Record, error) {
		completed++
		if expected.OperationID != newer.OperationID {
			t.Fatal("old history hid newer prepared candidate")
		}
		intent, err := projectx.LoadReplacement(d.StateRoot, d.ID, "demo")
		if err != nil || intent.BackendObject != newer.CandidateBackend {
			t.Fatal("new candidate receipt missing")
		}
		s.Backend.ObjectID = newer.CandidateBackend
		if err := session.SaveRecord(d.StateRoot, d.ID, s); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(filepath.Join(dir, "demo.json")); err != nil {
			t.Fatal(err)
		}
		b.SetObservation(backend.Observation{ObjectID: s.Backend.ObjectID, Exists: true, State: backend.ObjectStopped})
		return s, nil
	}
	if err := Run(t.Context(), append(prefix, "project", "rebuild", "retry", "demo"), o); err != nil {
		t.Fatal(err)
	}
	got, _ := projectx.Load(d.StateRoot, d.ID, "demo")
	if prepared != 1 || completed != 1 || got.BackendObject != newer.CandidateBackend || got.VolumeID != first.VolumeID {
		t.Fatal("retry did not retain newer candidate and original work")
	}
	// With both journals gone, an explicit completed retry settles only new history.
	o.AlphaRebuildPrepare = func(context.Context, config.Config, config.Domain, string, string, string) (session.RebuildJournal, error) {
		t.Fatal("completed retry prepared another system")
		return session.RebuildJournal{}, nil
	}
	o.AlphaRebuildCandidate = func(_ context.Context, _ config.Config, _ config.Domain, _ string, expected session.RebuildJournal) (session.Record, error) {
		if expected.OperationID != newer.OperationID {
			t.Fatal("completed retry used stale history")
		}
		return s, nil
	}
	if err := Run(t.Context(), append(prefix, "project", "rebuild", "retry", "demo"), o); err != nil {
		t.Fatal(err)
	}
}

func TestProjectRebuildPublicArgumentsAndPendingImport(t *testing.T) {
	for _, args := range [][]string{{"rebuild", "demo"}, {"rebuild", "--base", "registered-base", "demo"}, {"rebuild", "retry", "demo"}} {
		if _, err := parseProject(args); err != nil {
			t.Fatalf("valid args %v: %v", args, err)
		}
	}
	for _, args := range [][]string{{"rebuild", "retry", "--base", "current", "demo"}, {"rebuild", "--source", "/source", "demo"}, {"rebuild", "--base", "../unsafe", "demo"}, {"rebuild", "retry"}} {
		if _, err := parseProject(args); err == nil {
			t.Fatalf("invalid args accepted %v", args)
		}
	}
	prefix, d, o, _, _ := projectFixture(t)
	if err := Run(t.Context(), append(prefix, "project", "create", "demo"), o); err != nil {
		t.Fatal(err)
	}
	before, _ := projectx.Load(d.StateRoot, d.ID, "demo")
	before.ImportID = "00112233-4455-6677-8899-aabbccddeeff"
	before.ImportSource = "/private/source"
	if err := projectx.Save(d.StateRoot, d.ID, before); err != nil {
		t.Fatal(err)
	}
	o.AlphaRebuildPrepare = func(context.Context, config.Config, config.Domain, string, string, string) (session.RebuildJournal, error) {
		t.Fatal("pending import reached candidate mutation")
		return session.RebuildJournal{}, nil
	}
	o.AlphaRebuildCandidate = func(context.Context, config.Config, config.Domain, string, session.RebuildJournal) (session.Record, error) {
		t.Fatal("pending import rebuilt")
		return session.Record{}, nil
	}
	if err := Run(t.Context(), append(prefix, "project", "rebuild", "demo"), o); err == nil || !strings.Contains(err.Error(), "pending import") {
		t.Fatalf("pending import refusal: %v", err)
	}
	after, _ := projectx.Load(d.StateRoot, d.ID, "demo")
	if after != before {
		t.Fatal("refused rebuild changed project")
	}
}
