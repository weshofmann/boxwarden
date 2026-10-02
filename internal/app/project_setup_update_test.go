package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/weshofmann/boxwarden/internal/backend"
	"github.com/weshofmann/boxwarden/internal/config"
	"github.com/weshofmann/boxwarden/internal/projectx"
	"github.com/weshofmann/boxwarden/internal/workspacex"
)

func TestProjectSetupUpdateAdmitsAssetsAndRetainsExistingProject(t *testing.T) {
	prefix, d, o, _, out := projectFixture(t)
	if err := Run(t.Context(), append(prefix, "project", "create", "--size-mib", "16", "demo"), o); err != nil {
		t.Fatal(err)
	}
	prior, err := projectx.LoadSetup(d.StateRoot)
	if err != nil {
		t.Fatal(err)
	}
	command := append(prefix, "project", "setup-update", "--source-root", "/new/source", "--formatter-bundle", "/new/formatter", "--iso", "/new/ubuntu.iso", "--go", "/new/tool/go")
	if _, err := parseCommand(command, o); err != nil {
		t.Fatal(err)
	}
	r, err := projectx.Load(d.StateRoot, d.ID, "demo")
	if err != nil {
		t.Fatal(err)
	}
	r.ImportID = "11223344-5566-7788-99aa-bbccddeeff00"
	r.ImportSource = "/private/source"
	r.Imported = true
	if err := projectx.Save(d.StateRoot, d.ID, r); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(d.StateRoot, "projects", "demo.json")
	before, _ := os.ReadFile(path)
	called := 0
	o.ProjectSetupCheck = func(_ context.Context, actual config.Domain, s projectx.Setup) error {
		called++
		if actual.ID != d.ID || s.SourceRoot != "/new/source" {
			t.Fatal("incorrect admission inputs")
		}
		return errors.New("fixture admission failed")
	}
	if err := Run(t.Context(), command, o); err == nil || !strings.Contains(err.Error(), "previous setup retained") {
		t.Fatalf("admission failure: %v", err)
	}
	active, err := projectx.LoadSetup(d.StateRoot)
	if err != nil || active != prior {
		t.Fatal("failed admission changed setup")
	}
	o.ProjectSetupCheck = func(context.Context, config.Domain, projectx.Setup) error { called++; return nil }
	if err := Run(t.Context(), command, o); err != nil {
		t.Fatal(err)
	}
	if called != 2 {
		t.Fatalf("admission calls %d", called)
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("project binding changed")
	}
	if !strings.Contains(out.String(), "previous setup:") || !strings.Contains(out.String(), "no workspace or guest helper was changed") {
		t.Fatalf("missing outcome %s", out.String())
	}
	o.AlphaExport = func(_ context.Context, _ config.Domain, in AlphaExportInput, _ backend.Observer) (workspacex.ExportJournal, string, error) {
		if in.SourceRoot != "/new/source" || in.ISOPath != "/new/ubuntu.iso" || in.GoBinary != "/new/tool/go" || in.VolumeID != r.VolumeID || in.Selected[0] != "boxwarden-import-"+r.ImportID {
			t.Fatalf("export failed to use updated setup and original work: %+v", in)
		}
		return workspacex.ExportJournal{}, "", errors.New("captured updated exporter")
	}
	dest := filepath.Join(t.TempDir(), "returned")
	if err := Run(t.Context(), append(prefix, "project", "export", "--destination", dest, "demo"), o); err == nil || !strings.Contains(err.Error(), "captured updated exporter") {
		t.Fatalf("export: %v", err)
	}
}

func TestPublicListAfterSetupUpdateRemainsReadOnly(t *testing.T) {
	prefix, d, o, _, out := projectFixture(t)
	if err := Run(t.Context(), append(prefix, "project", "create", "--size-mib", "16", "demo"), o); err != nil {
		t.Fatal(err)
	}
	if err := Run(t.Context(), append(prefix, "project", "setup-update", "--source-root", "/new/source", "--formatter-bundle", "/new/formatter", "--iso", "/new/ubuntu.iso", "--go", "/new/tool/go"), o); err != nil {
		t.Fatal(err)
	}
	before := snapshotProjectTree(t, d.StateRoot)
	out.Reset()
	if err := Run(t.Context(), append(prefix, "project", "list"), o); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, snapshotProjectTree(t, d.StateRoot)) {
		t.Fatal("list after update changed tree")
	}
	if !strings.Contains(out.String(), "project: demo") || !strings.Contains(out.String(), "state: stopped") {
		t.Fatalf("list %s", out.String())
	}
}
