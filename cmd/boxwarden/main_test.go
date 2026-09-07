package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/weshofmann/boxwarden/internal/config"
	"github.com/weshofmann/boxwarden/internal/supervisor"
)

func TestProductionPublicOptionsWireAdmittedStarterFactory(t *testing.T) {
	options := publicOptions(&bytes.Buffer{})
	if options.SessionStarter != nil || options.SessionStarterFactory == nil {
		t.Fatal("production public start is not factory-composed")
	}
	if _, err := options.SessionStarterFactory(config.Config{}, config.Domain{}, "/private/config.json"); err == nil {
		t.Fatal("production factory accepted unadmitted configuration")
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
