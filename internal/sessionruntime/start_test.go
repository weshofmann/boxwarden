package sessionruntime

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/weshofmann/boxwarden/internal/backend"
	"github.com/weshofmann/boxwarden/internal/config"
	"github.com/weshofmann/boxwarden/internal/hostx"
	"github.com/weshofmann/boxwarden/internal/sshx"
	"github.com/weshofmann/boxwarden/internal/supervisor"
)

func TestProductionStartDependenciesUseExactAdmissionAndCheckOnlyCA(t *testing.T) {
	f := newFixture(t)
	loaded, err := config.Load(f.request.HostConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	selected, _ := loaded.Domain("work")
	if starter, err := NewStarter(loaded, selected, f.request.HostConfigPath); err != nil || starter == nil {
		t.Fatalf("production starter = %v, %v", starter, err)
	}
	deps, err := startDependencies(loaded, selected, f.request.HostConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if deps.ConfigPath != f.request.HostConfigPath || deps.RuntimeRoot != filepath.Join(f.root, "runtime") {
		t.Fatalf("locators = %#v", deps)
	}
	wantHost := hostx.Request{TartPath: f.tartPath, TartHome: f.tartHome, SoftnetPath: f.softnet, ConfiguredStateRoots: []string{f.personal, f.root}}
	if !reflect.DeepEqual(deps.HostRequest, wantHost) {
		t.Fatalf("host request = %#v", deps.HostRequest)
	}
	if !reflect.DeepEqual(deps.ConfiguredDomains, []sshx.Domain{{ID: "personal", StateRoot: f.personal}, {ID: "work", StateRoot: f.root}}) {
		t.Fatalf("CA domains = %#v", deps.ConfiguredDomains)
	}
	if _, ok := deps.Supervisor.(*supervisor.ExactController); !ok {
		t.Fatalf("controller = %T", deps.Supervisor)
	}
	if deps.Host == nil || deps.CA == nil {
		t.Fatal("missing admission checkers")
	}
	if _, ok := deps.CA.(interface {
		Init(context.Context, sshx.Domain, []sshx.Domain) (sshx.CAInitResult, error)
	}); ok {
		t.Fatal("start CA capability exposes initialization")
	}
	first, err := deps.NewGeneration()
	if err != nil {
		t.Fatal(err)
	}
	second, err := deps.NewGeneration()
	if err != nil || len(first) != 36 || first == second || first[14] != '4' || !strings.ContainsRune("89ab", rune(first[19])) {
		t.Fatalf("generation UUIDs = %q %q, %v", first, second, err)
	}
	before := time.Now()
	now := deps.Now()
	if now.Before(before) || now.After(time.Now()) {
		t.Fatal("clock is not current host time")
	}
	// Execute the exact configured observation path. No host qualification or
	// Tart mutation occurs; this script only validates argv and its closed env.
	script := "#!/bin/sh\n[ \"$*\" = 'list --format json' ] || exit 11\n[ \"$TART_HOME\" = '" + f.tartHome + "' ] || exit 12\n[ \"$PATH\" = /usr/bin:/bin ] || exit 13\n[ \"$LANG:$LC_ALL\" = C:C ] || exit 14\n[ -z \"$BOXWARDEN_TEST_SECRET\" ] || exit 15\nprintf '[{\"Name\":\"boxwarden-work-dev\",\"State\":\"stopped\",\"Accessed\":\"2026-09-07T00:00:00Z\",\"Source\":\"local\",\"Disk\":1,\"Size\":1,\"Running\":false}]'\n"
	if err := os.WriteFile(f.tartPath, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(f.tartPath, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BOXWARDEN_TEST_SECRET", "must-not-inherit")
	got, err := deps.Observer.Observe(context.Background(), "boxwarden-work-dev")
	if err != nil || got.ObjectID != "boxwarden-work-dev" || got.State != backend.ObjectStopped {
		t.Fatalf("qualified observation = %#v, %v", got, err)
	}
	for _, root := range []string{f.root, f.personal} {
		if _, err := os.Stat(filepath.Join(root, "runtime")); !os.IsNotExist(err) {
			t.Fatalf("construction mutated runtime: %v", err)
		}
	}
}

func TestNewStarterRejectsForeignDomainAndNoncanonicalConfigPath(t *testing.T) {
	f := newFixture(t)
	loaded, err := config.Load(f.request.HostConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	selected, _ := loaded.Domain("work")
	for _, path := range []string{"config.json", filepath.Dir(f.request.HostConfigPath) + "/./config.json"} {
		if _, err := NewStarter(loaded, selected, path); err == nil {
			t.Fatalf("accepted config path %q", path)
		}
	}
	selected.StateRoot = f.personal
	if _, err := NewStarter(loaded, selected, f.request.HostConfigPath); err == nil {
		t.Fatal("accepted foreign selected root")
	}
}

func TestRunRequestUsesAuthoritativeProductionOwner(t *testing.T) {
	f := newFixture(t)
	path := writeStartRequest(t, f.request)
	if err := os.Remove(f.request.HostConfigPath); err != nil {
		t.Fatal(err)
	}
	err := RunRequest(context.Background(), path)
	if err == nil || !strings.Contains(err.Error(), "reload host configuration") {
		t.Fatalf("production child did not reload authoritative config: %v", err)
	}
	if _, err := os.Stat(f.request.RuntimeDirectory); !os.IsNotExist(err) {
		t.Fatalf("admission failure did not clean exact generation: %v", err)
	}
}

func writeStartRequest(t *testing.T, request supervisor.LaunchRequest) string {
	t.Helper()
	if err := os.MkdirAll(request.RuntimeDirectory, 0700); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(request.RuntimeDirectory, "supervisor-request.json")
	for name, contents := range map[string][]byte{path: data, filepath.Join(request.RuntimeDirectory, "generation.lock"): nil} {
		if err := os.WriteFile(name, contents, 0600); err != nil {
			t.Fatal(err)
		}
	}
	return path
}
