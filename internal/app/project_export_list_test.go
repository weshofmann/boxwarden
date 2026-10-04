package app

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/weshofmann/boxwarden/internal/projectx"
	"github.com/weshofmann/boxwarden/internal/workspacex"
)

func TestProjectExportListIsReadOnlyWithoutBackendOrRecovery(t *testing.T) {
	prefix, d, o, _, out := projectFixture(t)
	if err := Run(t.Context(), append(prefix, "project", "create", "demo"), o); err != nil {
		t.Fatal(err)
	}
	r, err := projectx.Load(d.StateRoot, d.ID, "demo")
	if err != nil {
		t.Fatal(err)
	}
	r.ImportID = "00112233-4455-4677-8899-aabbccddeeff"
	r.ImportSource = "/private/source"
	r.Imported = true
	if err := projectx.Save(d.StateRoot, d.ID, r); err != nil {
		t.Fatal(err)
	}
	phases := []workspacex.ExportPhase{workspacex.ExportCopying, workspacex.ExportSnapshotReady, workspacex.ExportInspected, workspacex.ExportPublished}
	for i, phase := range phases {
		j := projectRetryJournal(r, d, "/unavailable/returned files", phase)
		j.ID = fmt.Sprintf("00112233-4455-4677-8899-%012d", i+1)
		j.SnapshotPath = filepath.Join("exports", j.ID, "snapshot.raw")
		if i == 2 {
			j.BackendObject = "historical-owned-backend"
		}
		saveProjectRetryJournal(t, d, j)
	}
	foreign := projectRetryJournal(r, d, "/unavailable/unrelated", workspacex.ExportCopying)
	foreign.ID = "00112233-4455-4677-8899-000000000099"
	foreign.SnapshotPath = filepath.Join("exports", foreign.ID, "snapshot.raw")
	foreign.VolumeID = "aabbccdd-4455-4677-8899-000000000099"
	saveProjectRetryJournal(t, d, foreign)
	// Current sessions/workspaces and rebuild metadata are not discovery inputs.
	if err := os.RemoveAll(filepath.Join(d.StateRoot, "sessions")); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(d.StateRoot, "workspaces")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d.StateRoot, "projects", ".replacement-demo.json"), []byte("pending-rebuild-metadata"), 0o600); err != nil {
		t.Fatal(err)
	}
	before := snapshotProjectTree(t, d.StateRoot)
	out.Reset()
	readOnly := Options{Output: out, storageCheck: syntheticStorageCheck}
	if err := Run(t.Context(), append(prefix, "project", "export", "list", "demo"), readOnly); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"exports: recorded journal snapshot", "recorded phase: copying", "recorded phase: snapshot-ready", "recorded phase: inspected", "recorded phase: published", "bookmark binding: matches", "bookmark binding: historical", "project export retry --transaction", `destination parent: "/unavailable/returned files"`, "recorded project files:"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("listing missing %q: %s", want, out)
		}
	}
	if strings.Contains(out.String(), foreign.ID) || strings.Contains(out.String(), "readiness: ready") {
		t.Fatalf("foreign or readiness claim: %s", out)
	}
	if !reflect.DeepEqual(before, snapshotProjectTree(t, d.StateRoot)) {
		t.Fatal("listing changed files, modes or metadata")
	}
}

func TestProjectExportListEmptyAndCorruptRegistryDoNotActOrPrintPartial(t *testing.T) {
	prefix, d, o, _, out := projectFixture(t)
	if err := Run(t.Context(), append(prefix, "project", "create", "demo"), o); err != nil {
		t.Fatal(err)
	}
	readOnly := Options{Output: out, storageCheck: syntheticStorageCheck}
	out.Reset()
	if err := Run(t.Context(), append(prefix, "project", "export", "list", "demo"), readOnly); err != nil || !strings.Contains(out.String(), "no retained export transactions") {
		t.Fatalf("empty list: %v %s", err, out)
	}
	r, err := projectx.Load(d.StateRoot, d.ID, "demo")
	if err != nil {
		t.Fatal(err)
	}
	j := projectRetryJournal(r, d, "/unavailable/destination", workspacex.ExportCopying)
	j.Selected = []string{"synthetic"}
	saveProjectRetryJournal(t, d, j)
	bad := filepath.Join(d.StateRoot, "exports", "ffffffff-4455-4677-8899-000000000099.json")
	if err := os.WriteFile(bad, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	before := snapshotProjectTree(t, d.StateRoot)
	out.Reset()
	if err := Run(t.Context(), append(prefix, "project", "export", "list", "demo"), readOnly); err == nil || out.Len() != 0 {
		t.Fatalf("corruption hid or printed partial: %v %s", err, out)
	}
	if !reflect.DeepEqual(before, snapshotProjectTree(t, d.StateRoot)) {
		t.Fatal("failed listing changed runtime state")
	}
}

func TestProjectExportListQuotesRecordedPathsAndTakesNoSelectionOptions(t *testing.T) {
	prefix, d, o, _, out := projectFixture(t)
	if err := Run(t.Context(), append(prefix, "project", "create", "demo"), o); err != nil {
		t.Fatal(err)
	}
	r, err := projectx.Load(d.StateRoot, d.ID, "demo")
	if err != nil {
		t.Fatal(err)
	}
	j := projectRetryJournal(r, d, "/unavailable/path\nforged-phase", workspacex.ExportCopying)
	j.Selected = []string{"synthetic"}
	saveProjectRetryJournal(t, d, j)
	out.Reset()
	if err := Run(t.Context(), append(prefix, "project", "export", "list", "demo"), Options{Output: out, storageCheck: syntheticStorageCheck}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "\nforged-phase") || !strings.Contains(out.String(), `path\nforged-phase`) {
		t.Fatalf("recorded path forged output: %q", out.String())
	}
	for _, args := range [][]string{{"export", "list"}, {"export", "list", "--destination", "/new/path", "demo"}, {"export", "list", "--transaction", j.ID, "demo"}, {"export", "list", "demo", "other"}} {
		if _, err := parseProject(args); err == nil {
			t.Fatalf("ambiguous listing accepted: %v", args)
		}
	}
	if _, err := parseProject([]string{"export", "list", "demo"}); err != nil {
		t.Fatal(err)
	}
}
