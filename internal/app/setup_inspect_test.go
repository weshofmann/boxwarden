package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/weshofmann/boxwarden/internal/config"
	"github.com/weshofmann/boxwarden/internal/hostidentity"
	"github.com/weshofmann/boxwarden/internal/hostx"
	"github.com/weshofmann/boxwarden/internal/projectx"
	"github.com/weshofmann/boxwarden/internal/sshx"
)

type setupDoctor struct {
	status hostx.Status
	calls  int
}

func (d *setupDoctor) Doctor(context.Context, hostx.Request) hostx.Report {
	d.calls++
	return hostx.Report{Status: d.status}
}

// Removing the pre-load inspection route would turn recoverable setup states
// into process errors and lose the JSON contract used by native onboarding.
func inspectSetupTest(t *testing.T, path string, o Options) map[string]any {
	t.Helper()
	var out bytes.Buffer
	o.Output = &out
	if err := Run(t.Context(), []string{"--config", path, "setup", "inspect", "--json"}, o); err != nil {
		t.Fatalf("inspect: %v; output %s", err, out.String())
	}
	var got map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["version"] != float64(1) || got["scope"] != "alpha_project_setup" {
		t.Fatalf("contract: %v", got)
	}
	return got
}

func TestSetupInspectConfigFailuresAreStructuredAndReadOnly(t *testing.T) {
	base, _ := filepath.EvalSymlinks(t.TempDir())
	invalid := filepath.Join(base, "invalid.json")
	if err := os.WriteFile(invalid, []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "link.json")
	if err := os.Symlink(invalid, link); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct{ path, status string }{
		{filepath.Join(base, "missing.json"), "config_missing"}, {invalid, "config_invalid"}, {link, "config_location_inadmissible"}, {"relative.json", "config_location_inadmissible"},
		{"", "config_location_inadmissible"},
	} {
		t.Run(tt.status+tt.path, func(t *testing.T) {
			doctor := &setupDoctor{status: hostx.Healthy}
			got := inspectSetupTest(t, tt.path, Options{HostDoctor: doctor})
			if got["status"] != tt.status || got["config_valid"] != false || got["selection_acceptable"] != false {
				t.Fatalf("got %v", got)
			}
			if doctor.calls != 0 {
				t.Fatal("host inspection ran before configuration admission")
			}
		})
	}
	if _, err := os.Stat(filepath.Join(base, "missing.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("inspection created config")
	}
}

func TestSetupInspectAdmittedPrerequisiteStates(t *testing.T) {
	for _, tt := range []struct {
		name                    string
		host                    hostx.Status
		storage, assets, domain error
		saved                   bool
		want                    string
	}{
		{name: "config backing filesystem", host: hostx.Healthy, storage: hostidentity.ErrConfigLocationInadmissible, want: "config_location_inadmissible"},
		{name: "storage", host: hostx.Healthy, storage: errors.New("identity mismatch"), want: "workspace_storage_unavailable"},
		{name: "host missing", host: hostx.Missing, want: "host_tools_uninitialized"},
		{name: "host unsafe", host: hostx.Drifted, want: "host_tools_incompatible"},
		{name: "host unsupported", host: hostx.Unsupported, want: "host_tools_incompatible"},
		{name: "domain missing", host: hostx.Healthy, domain: sshx.ErrCAMissing, want: "domain_uninitialized"},
		{name: "domain unsafe", host: hostx.Healthy, domain: errors.New("domain CA mismatch"), want: "domain_incompatible"},
		{name: "setup missing", host: hostx.Healthy, want: "project_setup_missing"},
		{name: "setup assets", host: hostx.Healthy, saved: true, assets: errors.New("pinned asset changed"), want: "project_setup_invalid"},
		{name: "ready empty projects", host: hostx.Healthy, saved: true, want: "ready"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			path, d := writeV2DomainFixture(t, "alpha")
			loaded, err := config.Load(path)
			if err != nil {
				t.Fatal(err)
			}
			raw, err := loaded.EnrolledCopy("alpha", config.WorkspaceStorage{MountPoint: d.StateRoot, VolumeUUID: "12345678-1234-1234-1234-123456789abc"})
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, raw, 0600); err != nil {
				t.Fatal(err)
			}
			if tt.saved {
				if err := projectx.SaveSetup(d.StateRoot, projectx.Setup{Version: 1, SourceRoot: "/source", FormatterBundle: "/formatter", ISOPath: "/ubuntu.iso", GoBinary: "/tool/go"}); err != nil {
					t.Fatal(err)
				}
			}
			doctor := &setupDoctor{status: tt.host}
			got := inspectSetupTest(t, path, Options{HostDoctor: doctor, DomainSetupCheck: func(context.Context, config.Config, config.Domain) (bool, error) {
				if errors.Is(tt.domain, sshx.ErrCAMissing) {
					return false, nil
				}
				return tt.domain == nil, tt.domain
			}, storageCheck: func(hostidentity.StorageExpectation) error { return tt.storage }, ProjectSetupCheck: func(context.Context, config.Domain, projectx.Setup) error { return tt.assets }})
			if got["status"] != tt.want || got["config_valid"] != true {
				t.Fatalf("got %v", got)
			}
			if tt.want == "ready" && (got["setup_version"] != float64(1) || got["recipe_preparation_available"] != false) {
				t.Fatalf("legacy readiness metadata %v", got)
			}
			if got["selection_acceptable"] != (tt.storage == nil) {
				t.Fatalf("selection %v", got)
			}
			if _, err := os.Stat(filepath.Join(d.StateRoot, "locks")); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("inspection created locks")
			}
		})
	}
}

func TestSetupInspectReportsMissingEnrolledRootBeforeLoadingHost(t *testing.T) {
	path, d := writeV2DomainFixture(t, "alpha")
	loaded, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := loaded.EnrolledCopy("alpha", config.WorkspaceStorage{MountPoint: d.StateRoot, VolumeUUID: "12345678-1234-1234-1234-123456789abc"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(d.StateRoot, "absent-volume")
	raw = bytes.ReplaceAll(raw, []byte(d.StateRoot), []byte(missing))
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	got := inspectSetupTest(t, path, Options{})
	if got["status"] != "workspace_storage_unavailable" || got["config_valid"] != false {
		t.Fatalf("got %v", got)
	}
}

func TestSetupInspectRejectsDirectoryConfigLocationAndMalformedInvocation(t *testing.T) {
	path, _ := filepath.EvalSymlinks(t.TempDir())
	got := inspectSetupTest(t, path, Options{})
	if got["status"] != "config_location_inadmissible" {
		t.Fatalf("got %v", got)
	}
	for _, args := range [][]string{{"setup", "inspect"}, {"--domain", "alpha", "setup", "inspect", "--json"}, {"setup", "inspect", "--json", "extra"}} {
		if err := Run(t.Context(), args, Options{Output: &bytes.Buffer{}}); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
}
