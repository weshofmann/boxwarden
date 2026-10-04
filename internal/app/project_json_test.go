package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/weshofmann/boxwarden/internal/backend"
	"github.com/weshofmann/boxwarden/internal/config"
	"github.com/weshofmann/boxwarden/internal/projectx"
	"github.com/weshofmann/boxwarden/internal/session"
	"github.com/weshofmann/boxwarden/internal/supervisor"
	"github.com/weshofmann/boxwarden/internal/workspacex"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func jsonEvents(t *testing.T, out *bytes.Buffer) []map[string]any {
	t.Helper()
	var events []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatalf("non-JSON stdout %q: %v", line, err)
		}
		if event["version"] != float64(1) {
			t.Fatalf("missing version: %#v", event)
		}
		events = append(events, event)
	}
	return events
}
func TestProjectJSONParserAndPrerequisiteErrors(t *testing.T) {
	for _, args := range [][]string{{"list", "--json"}, {"open", "--json", "demo"}, {"import", "retry", "--json", "demo"}, {"rebuild", "retry", "--json", "demo"}} {
		if _, err := parseProject(args); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
	}
	for _, args := range [][]string{{"--config", "/missing/config.json", "--domain", "alpha", "project", "list", "--json"}, {"--domain", "alpha", "project", "create", "--json", "--size-mib", "0", "demo"}} {
		out := &bytes.Buffer{}
		if err := Run(t.Context(), args, Options{Output: out}); err == nil {
			t.Fatal("expected error")
		}
		events := jsonEvents(t, out)
		last := events[len(events)-1]
		if last["type"] != "error" || last["message"] == "" || last["data"].(map[string]any)["uncertain"] != true {
			t.Fatalf("missing useful failure: %#v", last)
		}
	}
}
func TestProjectJSONEmptyListExplainsMissingSetup(t *testing.T) {
	path, _ := writeV2DomainFixture(t, "alpha")
	out := &bytes.Buffer{}
	if err := Run(t.Context(), []string{"--config", path, "--domain", "alpha", "project", "list", "--json"}, Options{Output: out, storageCheck: syntheticStorageCheck}); err != nil {
		t.Fatal(err)
	}
	events := jsonEvents(t, out)
	last := events[len(events)-1]
	data := last["data"].(map[string]any)
	if last["type"] != "result" || last["operation"] != "project.list" || data["setup"].(map[string]any)["status"] != "missing" || len(data["projects"].([]any)) != 0 {
		t.Fatalf("empty setup hidden: %#v", last)
	}
}
func TestProjectJSONCreateAndListUseActualBindings(t *testing.T) {
	prefix, d, o, _, out := projectFixture(t)
	out.Reset()
	if err := Run(t.Context(), append(prefix, "project", "create", "--json", "--size-mib", "16", "demo"), o); err != nil {
		t.Fatal(err)
	}
	events := jsonEvents(t, out)
	if events[len(events)-1]["type"] != "result" {
		t.Fatalf("missing terminal result: %#v", events)
	}
	out.Reset()
	before := snapshotProjectTree(t, d.StateRoot)
	if err := Run(t.Context(), append(prefix, "project", "list", "--json"), o); err != nil {
		t.Fatal(err)
	}
	events = jsonEvents(t, out)
	project := events[len(events)-1]["data"].(map[string]any)["projects"].([]any)[0].(map[string]any)
	if project["name"] != "demo" || project["management_ready"] != false || project["workspace"].(map[string]any)["size_bytes"] != float64(16<<20) {
		t.Fatalf("wrong snapshot: %#v", project)
	}
	after := snapshotProjectTree(t, d.StateRoot)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("list writes state")
	}
}

func TestProjectJSONPreviewReturnsExactSelection(t *testing.T) {
	path, d := writeV2DomainFixture(t, "alpha")
	source, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(source, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(source, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "README.md"), []byte("selected\n"), 0600); err != nil {
		t.Fatal(err)
	}
	out := &bytes.Buffer{}
	before := snapshotProjectTree(t, d.StateRoot)
	if err := Run(t.Context(), []string{"--config", path, "--domain", "alpha", "project", "import", "preview", "--json", "--source", source, "--exclude", ".git"}, Options{Output: out}); err != nil {
		t.Fatal(err)
	}
	events := jsonEvents(t, out)
	terminal := events[len(events)-1]
	data := terminal["data"].(map[string]any)
	if terminal["operation"] != "project.import.preview" || data["file_count"] != float64(1) || data["total_bytes"] != float64(9) || len(data["digest"].(string)) != 64 || data["exclusions"].([]any)[0] != ".git" || data["entries"].([]any)[0].(map[string]any)["path"] != "README.md" {
		t.Fatalf("wrong preview: %#v", data)
	}
	if !reflect.DeepEqual(before, snapshotProjectTree(t, d.StateRoot)) {
		t.Fatal("preview writes state")
	}
}

func TestProjectJSONExportRetryAndDiscoveryReturnAdmittedReceipt(t *testing.T) {
	for _, phase := range []workspacex.ExportPhase{workspacex.ExportSnapshotReady, workspacex.ExportCopying} {
		t.Run(string(phase), func(t *testing.T) {
			prefix, d, o, _, out := projectFixture(t)
			if err := Run(t.Context(), append(prefix, "project", "create", "demo"), o); err != nil {
				t.Fatal(err)
			}
			r, _ := projectx.Load(d.StateRoot, d.ID, "demo")
			r.ImportID = "00112233-4455-6677-8899-aabbccddeeff"
			r.ImportSource = "/private/source"
			r.Imported = true
			if err := projectx.Save(d.StateRoot, d.ID, r); err != nil {
				t.Fatal(err)
			}
			j := projectRetryJournal(r, d, filepath.Join(t.TempDir(), "destination"), phase)
			saveProjectRetryJournal(t, d, j)
			out.Reset()
			if err := Run(t.Context(), append(prefix, "project", "export", "list", "--json", "demo"), o); err != nil {
				t.Fatal(err)
			}
			events := jsonEvents(t, out)
			entries := events[len(events)-1]["data"].(map[string]any)["exports"].([]any)
			entry := entries[0].(map[string]any)
			if entry["transaction"] != j.ID || entry["matches_current_bookmark"] != true || entry["retry_available"] != true {
				t.Fatalf("wrong discovery: %#v", entry)
			}
			o.AlphaExportResume = func(context.Context, config.Domain, AlphaExportResumeInput, backend.Observer) (workspacex.ExportJournal, string, error) {
				next := j
				if phase == workspacex.ExportCopying {
					next.Phase = workspacex.ExportAborted
					return next, "", nil
				}
				next.Phase = workspacex.ExportPublished
				return next, filepath.Join(j.DestinationParent, strings.ReplaceAll(j.ID, "-", "")), nil
			}
			out.Reset()
			if err := Run(t.Context(), append(prefix, "project", "export", "retry", "--json", "--transaction", j.ID, "demo"), o); err != nil {
				t.Fatal(err)
			}
			events = jsonEvents(t, out)
			data := events[len(events)-1]["data"].(map[string]any)
			if data["transaction"] != j.ID || (phase == workspacex.ExportCopying && (data["phase"] != "aborted" || data["published"] != "" || data["project_files"] != "")) || (phase == workspacex.ExportSnapshotReady && data["project_files"] != filepath.Join(j.DestinationParent, strings.ReplaceAll(j.ID, "-", ""), "boxwarden-import-"+r.ImportID)) {
				t.Fatalf("wrong receipt: %#v", data)
			}
		})
	}
}

func TestProjectJSONCallbackWriterAndFailureCannotProduceResult(t *testing.T) {
	out := &bytes.Buffer{}
	args := []string{"--config", "/missing/config.json", "--domain", "alpha", "project", "create", "--json", "demo"}
	output := ProjectOutput(args, out)
	fmt.Fprintln(output, "preparation: exact callback phase")
	if err := Run(t.Context(), args, Options{Output: output}); err == nil {
		t.Fatal("missing failure")
	}
	events := jsonEvents(t, out)
	if len(events) != 2 || events[0]["type"] != "progress" || events[1]["type"] != "error" {
		t.Fatalf("callback boundary: %#v", events)
	}
}

func TestProjectJSONReadyRequiresFreshExactSupervisor(t *testing.T) {
	prefix, d, o, b, out := projectFixture(t)
	if err := Run(t.Context(), append(prefix, "project", "create", "demo"), o); err != nil {
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
	check := func(ready bool) {
		out.Reset()
		if err := Run(t.Context(), append(prefix, "project", "list", "--json"), o); err != nil {
			t.Fatal(err)
		}
		events := jsonEvents(t, out)
		project := events[len(events)-1]["data"].(map[string]any)["projects"].([]any)[0].(map[string]any)
		if project["management_ready"] != ready || project["backend_running"] != true || project["observed_state"] != "running" {
			t.Fatalf("readiness confused with running: %#v", project)
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

func TestProjectJSONPostEffectObservationFailurePreservesSuccess(t *testing.T) {
	prefix, _, o, _, out := projectFixture(t)
	if err := Run(t.Context(), append(prefix, "project", "create", "demo"), o); err != nil {
		t.Fatal(err)
	}
	calls := 0
	o.BackendFactory = func(config.Config, config.Domain) (BackendDependencies, error) {
		calls++
		if calls > 1 {
			return BackendDependencies{}, errors.New("post-effect observer unavailable")
		}
		return BackendDependencies{Observer: o.Observer, Creator: o.Creator}, nil
	}
	out.Reset()
	if err := Run(t.Context(), append(prefix, "project", "status", "--json", "demo"), o); err != nil {
		t.Fatal(err)
	}
	events := jsonEvents(t, out)
	last := events[len(events)-1]
	project := last["data"].(map[string]any)["project"].(map[string]any)
	if last["type"] != "result" || project["state"] != "unavailable" || project["management_ready"] != false {
		t.Fatalf("post-effect uncertainty obscured success: %#v", last)
	}
}

func TestProjectJSONReportsActualWorkspacePhaseBeforeDriver(t *testing.T) {
	prefix, _, o, _, out := projectFixture(t)
	original := o.AlphaWorkspaceCreate
	o.AlphaWorkspaceCreate = func(ctx context.Context, d config.Domain, path string, in AlphaWorkspaceCreateInput) (workspacex.Record, error) {
		if !strings.Contains(out.String(), "Formatting the managed workspace") {
			t.Fatal("workspace driver entered without phase event")
		}
		return original(ctx, d, path, in)
	}
	out.Reset()
	if err := Run(t.Context(), append(prefix, "project", "create", "--json", "demo"), o); err != nil {
		t.Fatal(err)
	}
	jsonEvents(t, out)
}

func TestProjectJSONBoundsDetachedOutputAndRetainsTerminalError(t *testing.T) {
	out := &bytes.Buffer{}
	stream := &projectJSON{output: out, operation: "project.list"}
	stream.data = map[string]any{"oversized": strings.Repeat("x", 8<<20)}
	if err := stream.emit("result", "", stream.data); err == nil {
		t.Fatal("unbounded result emitted")
	}
	if err := stream.emit("error", "structured output exceeds limit", map[string]any{"uncertain": true}); err != nil {
		t.Fatal(err)
	}
	events := jsonEvents(t, out)
	if len(events) != 1 || events[0]["type"] != "error" {
		t.Fatal("oversized result leaked")
	}
}

func TestProjectJSONDetectionUsesParsedFlagsNotLiteralValues(t *testing.T) {
	for _, tc := range []struct {
		args      []string
		enabled   bool
		operation string
	}{
		{[]string{"project", "import", "preview", "--exclude", "--json", "--source", "/private/source"}, false, "project.import.preview"},
		{[]string{"project", "import", "preview", "--json", "--exclude", "--json=false", "--source", "/private/source"}, true, "project.import.preview"},
		{[]string{"project", "status", "-json", "list"}, true, "project.status"},
		{[]string{"project", "list", "--json=1"}, true, "project.list"},
		{[]string{"project", "list", "--json", "--json=false"}, false, "project.list"},
		{[]string{"--config", "project", "--domain", "alpha", "project", "list", "--json"}, true, "project.list"},
		{[]string{"project", "create", "--json", "--size-mib", "0", "demo"}, true, "project.create"},
	} {
		operation, enabled := projectJSONRequest(tc.args)
		if operation != tc.operation || enabled != tc.enabled {
			t.Fatalf("%v => %s %v, want %s %v", tc.args, operation, enabled, tc.operation, tc.enabled)
		}
	}
}
