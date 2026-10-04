package app

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/weshofmann/boxwarden/internal/config"
	"github.com/weshofmann/boxwarden/internal/importx"
	"github.com/weshofmann/boxwarden/internal/projectx"
	"github.com/weshofmann/boxwarden/internal/supervisor"
	"github.com/weshofmann/boxwarden/internal/workspacex"
)

func TestProjectImportPreviewIsReadOnlyWithoutProjectOrBackend(t *testing.T) {
	configPath, d := writeV2DomainFixture(t, "alpha")
	source, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(source, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(source, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{"README.md": "original\n", ".git/HEAD": "excluded\n"} {
		if err := os.WriteFile(filepath.Join(source, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	beforeState, beforeSource := snapshotProjectTree(t, d.StateRoot), snapshotProjectTree(t, source)
	out := &bytes.Buffer{}
	// No observer, creator, starter, importer, setup or named project is available.
	if err := Run(t.Context(), []string{"--config", configPath, "--domain", "alpha", "project", "import", "preview", "--source", source, "--exclude", ".git"}, Options{Output: out}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"selected: 1 files", "selection digest:", "README.md", "excluded: .git"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("preview missing %q: %s", want, out.String())
		}
	}
	if !reflect.DeepEqual(beforeState, snapshotProjectTree(t, d.StateRoot)) || !reflect.DeepEqual(beforeSource, snapshotProjectTree(t, source)) {
		t.Fatal("preview changed runtime or source tree")
	}
}

func TestProjectImportRetryKeepsFrozenSelectionBeforeCapture(t *testing.T) {
	prefix, d, o, _, _ := projectFixture(t)
	if err := Run(t.Context(), append(prefix, "project", "create", "--size-mib", "16", "selected"), o); err != nil {
		t.Fatal(err)
	}
	want, _ := importx.CanonicalSelection(importx.Selection{Excludes: []string{".git", "node_modules"}, ExpectedDigest: strings.Repeat("b", 64)})
	calls := 0
	o.AlphaImport = func(_ context.Context, _ config.Domain, input AlphaImportInput) (workspacex.ImportJournal, supervisor.ImportResult, error) {
		calls++
		r, err := projectx.Load(d.StateRoot, d.ID, "selected")
		if err != nil || r.Version != 3 || r.ImportSelection != want || r.ImportSource != "/private/source" || input.SourcePath != r.ImportSource || input.Selection != want || input.Resume {
			t.Fatalf("capture intent not frozen first: %#v %#v %v", r, input, err)
		}
		return workspacex.ImportJournal{}, supervisor.ImportResult{}, errors.New("synthetic pre-capture failure")
	}
	first := append(prefix, "project", "import", "--source", "/private/source", "--exclude", "node_modules", "--exclude", ".git", "--expected-digest", strings.Repeat("b", 64), "selected")
	if err := Run(t.Context(), first, o); err == nil || !strings.Contains(err.Error(), "retained selection") {
		t.Fatalf("first failure: %v", err)
	}
	if err := Run(t.Context(), append(prefix, "project", "import", "retry", "selected"), o); err == nil || !strings.Contains(err.Error(), "retained selection") {
		t.Fatalf("retry failure: %v", err)
	}
	if calls != 2 {
		t.Fatalf("capture calls: %d", calls)
	}
	for _, flags := range [][]string{{"--source", "/private/other"}, {"--exclude", "dist"}, {"--expected-digest", strings.Repeat("c", 64)}} {
		args := append(append(append([]string{}, prefix...), "project", "import", "retry"), flags...)
		args = append(args, "selected")
		if err := Run(t.Context(), args, o); err == nil {
			t.Fatalf("retry changed selection: %v", flags)
		}
	}
	if calls != 2 {
		t.Fatal("rejected retry reached capture")
	}
}

func TestProjectSelectionCommandAndWorkspaceBounds(t *testing.T) {
	for _, args := range [][]string{
		{"import", "preview", "--source", "/private/source", "--exclude", "node_modules"},
		{"import", "--source", "/private/source", "--exclude", "dist", "--expected-digest", strings.Repeat("a", 64), "demo"},
		{"create", "--size-mib", "2048", "demo"},
		{"create", "--size-mib", "4096", "demo"},
	} {
		if _, err := parseProject(args); err != nil {
			t.Fatalf("operator command %v: %v", args, err)
		}
	}
	for _, args := range [][]string{
		{"import", "preview", "--source", "/private/source", "demo"},
		{"import", "preview", "retry"},
		{"import", "--source", "/private/source", "--exclude", "../escape", "demo"},
		{"import", "--source", "/private/source", "--expected-digest", "wrong", "demo"},
		{"create", "--size-mib", "4097", "demo"},
	} {
		if _, err := parseProject(args); err == nil {
			t.Fatalf("unsafe command accepted: %v", args)
		}
	}
}

func TestProjectImportReceiptCannotDisagreeWithPreviewPin(t *testing.T) {
	input := AlphaImportInput{TransactionID: "10213243-5465-4768-899a-bbccddeeff00", VolumeID: "00112233-4455-4677-8899-aabbccddeeff", SessionName: "dev"}
	input.Selection, _ = importx.CanonicalSelection(importx.Selection{ExpectedDigest: strings.Repeat("a", 64)})
	journal := workspacex.ImportJournal{ID: input.TransactionID, Domain: "alpha", VolumeID: input.VolumeID, SessionName: input.SessionName, MountPath: projectMount, Phase: workspacex.ImportTransferring, SourceDigest: strings.Repeat("b", 64), FileCount: 1, TotalBytes: 7}
	receipt := supervisor.ImportResult{Digest: journal.SourceDigest, FileCount: journal.FileCount, TotalBytes: journal.TotalBytes, RemotePath: projectMount + "/boxwarden-import-" + journal.ID}
	var output bytes.Buffer
	if err := writeAlphaImport(&output, config.Domain{ID: "alpha"}, input, journal, receipt); err == nil || !strings.Contains(err.Error(), "differs from preview") || output.Len() != 0 {
		t.Fatalf("wrong pinned receipt accepted: %v %q", err, output.String())
	}
	journal.SourceDigest = strings.Repeat("a", 64)
	receipt.Digest = journal.SourceDigest
	if err := writeAlphaImport(&output, config.Domain{ID: "alpha"}, input, journal, receipt); err != nil {
		t.Fatalf("matching pinned receipt rejected: %v", err)
	}
}
