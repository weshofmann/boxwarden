package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/weshofmann/boxwarden/internal/app"
	"github.com/weshofmann/boxwarden/internal/backend"
	"github.com/weshofmann/boxwarden/internal/backend/fake"
	"github.com/weshofmann/boxwarden/internal/config"
	"github.com/weshofmann/boxwarden/internal/golden"
	"github.com/weshofmann/boxwarden/internal/session"
	"github.com/weshofmann/boxwarden/internal/supervisor"
)

// Production must use the loaded namespace for every public read and mutation,
// even when PATH, TART_HOME, and unrelated ambient environment disagree.
func TestProductionPublicCommandsUseConfiguredTartNamespace(t *testing.T) {
	for _, command := range []string{"register", "create", "status"} {
		t.Run(command, func(t *testing.T) {
			base, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			state, home, ambient := filepath.Join(base, "work"), filepath.Join(base, "tart-home"), filepath.Join(base, "ambient")
			for _, path := range []string{state, home, ambient} {
				if err := os.Mkdir(path, 0700); err != nil {
					t.Fatal(err)
				}
			}
			executable, softnet, path := filepath.Join(base, "qualified-tart"), filepath.Join(base, "softnet"), filepath.Join(base, "config.json")
			// The test executable implements only the fixed public list/clone/set
			// protocol. It refuses the wrong namespace before any side effect.
			script := fmt.Sprintf(`#!/bin/sh
set -eu
test "$0" = %q
test "$TART_HOME" = %q
test "$PATH" = /usr/bin:/bin
test "$LANG" = C
test "$LC_ALL" = C
test "${BOXWARDEN_TEST_AMBIENT-unset}" = unset
printf '%%s\n' "$*" >> "$TART_HOME/commands"
entry() {
  printf '{"Name":"%%s","State":"stopped","Running":false,"Accessed":"2026-09-07T00:00:00Z","Source":"local","Disk":1,"Size":1}' "$1"
}
case "$1" in
  list)
    test "$#" = 3; test "$2" = --format; test "$3" = json
    printf '['; entry golden-work-r1
    if test -f "$TART_HOME/object"; then
      read -r object < "$TART_HOME/object"
      printf ','; entry "$object"
    fi
    printf ']'
    ;;
  clone)
    test "$#" = 3; test "$2" = golden-work-r1
    printf '%%s\n' "$3" > "$TART_HOME/object"
    ;;
  set)
    test "$#" = 3; test "$3" = --random-mac
    read -r object < "$TART_HOME/object"; test "$2" = "$object"
    ;;
  *) exit 92 ;;
esac
`, executable, home)
			files := map[string]string{
				executable: script, softnet: "fixture",
				filepath.Join(ambient, "tart"): "#!/bin/sh\necho PATH-selected-Tart >&2\nexit 91\n",
				path:                           fmt.Sprintf(`{"version":2,"host":{"tart_executable":%q,"tart_home":%q,"softnet_source":%q},"domains":{"work":{"state_root":%q}}}`, executable, home, softnet, state),
			}
			for name, contents := range files {
				if err := os.WriteFile(name, []byte(contents), 0700); err != nil {
					t.Fatal(err)
				}
			}
			t.Setenv("PATH", ambient)
			t.Setenv("TART_HOME", ambient)
			t.Setenv("BOXWARDEN_TEST_AMBIENT", "must-not-inherit")
			t.Setenv("LANG", "ambient")
			t.Setenv("LC_ALL", "ambient")
			loaded, err := config.Load(path)
			if err != nil {
				t.Fatal(err)
			}
			domain, _ := loaded.Domain("work")
			fixtureBackend := fake.New(backend.Observation{ObjectID: "golden-work-r1", Exists: true, State: backend.ObjectStopped})
			if command != "register" {
				if _, err := golden.Register(context.Background(), domain, "golden-work-r1", fixtureBackend); err != nil {
					t.Fatal(err)
				}
			}
			if command == "status" {
				record, err := session.NewService(domain, fixtureBackend, fixtureBackend).Create(context.Background(), "dev", session.ModeClean)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(home, "object"), []byte(record.Backend.ObjectID+"\n"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			suffix := []string{"session", command, "dev"}
			if command == "register" {
				suffix = []string{"golden", "register", "golden-work-r1"}
			}
			var output bytes.Buffer
			if err := app.Run(context.Background(), append([]string{"--config", path, "--domain", "work"}, suffix...), publicOptions(&output)); err != nil {
				t.Fatalf("public %s failed: %v", command, err)
			}
			calls, err := os.ReadFile(filepath.Join(home, "commands"))
			if err != nil || !strings.Contains(string(calls), "list --format json\n") {
				t.Fatalf("configured namespace calls = %q, %v", calls, err)
			}
			if command == "create" {
				record, err := session.LoadRecord(state, "work", "dev")
				if err != nil || record.IntendedState != session.StateStopped {
					t.Fatalf("created record = %#v, %v", record, err)
				}
				for _, want := range []string{"clone golden-work-r1 " + record.Backend.ObjectID + "\n", "set " + record.Backend.ObjectID + " --random-mac\n"} {
					if strings.Count(string(calls), want) != 1 {
						t.Fatalf("calls %q lack one exact mutation %q", calls, want)
					}
				}
			}
		})
	}
}

func TestProductionPublicOptionsWireAdmittedStarterFactory(t *testing.T) {
	options := publicOptions(&bytes.Buffer{})
	if options.SessionStarter != nil || options.SessionStarterFactory == nil {
		t.Fatal("production public start is not factory-composed")
	}
	if _, err := options.SessionStarterFactory(config.Config{}, config.Domain{}, "/private/config.json"); err == nil {
		t.Fatal("production factory accepted unadmitted configuration")
	}
}

func TestProductionBackendFactoryRejectsMissingHostAndUnselectedDomain(t *testing.T) {
	options := publicOptions(&bytes.Buffer{})
	if options.Observer != nil || options.Creator != nil || options.BackendFactory == nil {
		t.Fatal("production backend is constructed before configuration admission")
	}
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "config.json")
	if err := os.WriteFile(path, []byte(fmt.Sprintf(`{"version":1,"domains":{"work":{"state_root":%q}}}`, root)), 0600); err != nil {
		t.Fatal(err)
	}
	loaded, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	selected, _ := loaded.Domain("work")
	for _, domain := range []config.Domain{selected, {}, {ID: selected.ID, StateRoot: filepath.Join(root, "other")}} {
		deps, err := options.BackendFactory(loaded, domain)
		if err == nil || deps.Observer != nil || deps.Creator != nil {
			t.Fatalf("factory accepted unavailable host or unselected domain: %#v, %v", deps, err)
		}
	}
}

func TestDefaultInternalSupervisorUsesAuthoritativeOwner(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	request := supervisor.LaunchRequest{Binding: supervisor.Binding{Domain: "work", SessionID: "session", BackendKind: "tart", BackendObject: "exact-vm", Generation: "generation"}, RuntimeDirectory: filepath.Join(root, "work", "session", "generation"), HostConfigPath: filepath.Join(root, "missing-config.json"), SessionRecordName: "dev"}
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
	handled, err := runInternal(context.Background(), []string{"internal", "session-supervisor", path}, nil, nil, nil)
	if !handled || err == nil || !strings.Contains(err.Error(), "reload host configuration") {
		t.Fatalf("production internal dispatch = %t, %v", handled, err)
	}
}

func TestRunInternalDispatchesOnlyExactHostInstallBeforePublicCLI(t *testing.T) {
	var output bytes.Buffer
	called := 0
	handled, err := runInternal(context.Background(), []string{"internal", "host-install"}, strings.NewReader(`{"version":1}`), &output, func(_ context.Context, input []byte) ([]byte, error) {
		called++
		if got, want := string(input), `{"version":1}`; got != want {
			t.Fatalf("root input = %q, want %q", got, want)
		}
		return []byte(`{"published":true,"already_installed":false,"refresh_login_session":false}`), nil
	})
	if err != nil || !handled {
		t.Fatalf("runInternal() = handled %t error %v, want handled success", handled, err)
	}
	if called != 1 {
		t.Fatalf("installer calls = %d, want 1", called)
	}
	if got, want := output.String(), `{"published":true,"already_installed":false,"refresh_login_session":false}`; got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

func TestRunInternalRejectsEveryOtherInternalArgvWithoutDispatch(t *testing.T) {
	for _, args := range [][]string{{"internal"}, {"internal", "other"}, {"internal", "host-install", "extra"}} {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			called := false
			handled, err := runInternal(context.Background(), args, strings.NewReader("ignored"), &bytes.Buffer{}, func(context.Context, []byte) ([]byte, error) {
				called = true
				return nil, nil
			})
			if !handled || err == nil || !strings.Contains(err.Error(), "unsupported internal command") {
				t.Fatalf("runInternal(%q) = handled %t error %v, want handled refusal", args, handled, err)
			}
			if called {
				t.Fatal("invalid internal argv reached root installer")
			}
		})
	}
}

func TestRunInternalLeavesPublicArgvAndStdinUntouched(t *testing.T) {
	reader := &panicReader{}
	handled, err := runInternal(context.Background(), []string{"--domain", "work", "doctor"}, reader, &bytes.Buffer{}, func(context.Context, []byte) ([]byte, error) {
		t.Fatal("public argv reached root installer")
		return nil, nil
	})
	if err != nil || handled {
		t.Fatalf("runInternal() = handled %t error %v, want unhandled", handled, err)
	}
}

func TestRunInternalDispatchesOnlyExactSessionSupervisor(t *testing.T) {
	called := ""
	handled, err := runInternal(context.Background(), []string{"internal", "session-supervisor", "/private/runtime/supervisor-request.json"}, strings.NewReader("ignored"), &bytes.Buffer{}, nil, func(_ context.Context, path string) error { called = path; return nil })
	if err != nil || !handled || called != "/private/runtime/supervisor-request.json" {
		t.Fatalf("session supervisor dispatch = handled %t path %q err %v", handled, called, err)
	}
	for _, args := range [][]string{{"internal", "session-supervisor"}, {"internal", "session-supervisor", "x", "extra"}, {"internal", "session-supervisor", "x", "y", "z"}} {
		handled, err := runInternal(context.Background(), args, strings.NewReader("ignored"), &bytes.Buffer{}, nil, func(context.Context, string) error { t.Fatal("invalid internal argv dispatched"); return nil })
		if !handled || err == nil {
			t.Fatalf("runInternal(%q) = handled %t error %v, want refusal", args, handled, err)
		}
	}
}

func TestRunInternalPropagatesBoundedReadInstallAndWriteFailures(t *testing.T) {
	tooLarge := strings.Repeat("x", 16<<10+1)
	handled, err := runInternal(context.Background(), []string{"internal", "host-install"}, strings.NewReader(tooLarge), &bytes.Buffer{}, func(context.Context, []byte) ([]byte, error) {
		t.Fatal("oversized input reached installer")
		return nil, nil
	})
	if !handled || err == nil || !strings.Contains(err.Error(), "bounded input") {
		t.Fatalf("oversized runInternal() = handled %t error %v", handled, err)
	}

	want := errors.New("root failure")
	handled, err = runInternal(context.Background(), []string{"internal", "host-install"}, strings.NewReader("{}"), &bytes.Buffer{}, func(context.Context, []byte) ([]byte, error) {
		return nil, want
	})
	if !handled || !errors.Is(err, want) {
		t.Fatalf("failing runInternal() = handled %t error %v, want root failure", handled, err)
	}

	handled, err = runInternal(context.Background(), []string{"internal", "host-install"}, strings.NewReader("{}"), shortWriter{}, func(context.Context, []byte) ([]byte, error) {
		return []byte("result"), nil
	})
	if !handled || !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("short-write runInternal() = handled %t error %v, want io.ErrShortWrite", handled, err)
	}
}

type panicReader struct{}

func (*panicReader) Read([]byte) (int, error) { panic("public command stdin was read") }

type shortWriter struct{}

func (shortWriter) Write(input []byte) (int, error) { return len(input) - 1, nil }
