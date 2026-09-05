package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
)

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
