package execx

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestOSRunnerCapturesBoundedOutput(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") == "1" {
		fmt.Fprint(os.Stdout, "abcdef")
		os.Exit(0)
	}

	runner := OSRunner{MaxOutputBytes: 3}
	result, err := runner.Run(context.Background(), Command{
		Path: os.Args[0],
		Args: []string{"-test.run=TestOSRunnerCapturesBoundedOutput"},
		Env:  []string{"GO_WANT_HELPER_PROCESS=1"},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if got, want := result.Stdout, "abc"; got != want {
		t.Fatalf("Run().Stdout = %q, want %q", got, want)
	}
	if !result.Truncated {
		t.Fatal("Run().Truncated = false, want true")
	}
}

func TestOSRunnerRejectsShellExecution(t *testing.T) {
	_, err := (OSRunner{}).Run(context.Background(), Command{Path: "sh", Args: []string{"-c", "echo unsafe"}})
	if err == nil {
		t.Fatal("Run(sh -c) error = nil, want rejection")
	}
}

func TestOSRunnerRejectsOversizedStdinWithoutLeakingBytes(t *testing.T) {
	const secret = "do-not-disclose"
	_, err := (OSRunner{MaxStdinBytes: len(secret) - 1}).Run(context.Background(), Command{
		Path:  os.Args[0],
		Args:  []string{"-test.run=^$"},
		Stdin: []byte(secret),
	})
	if err == nil {
		t.Fatal("Run() error = nil, want oversized stdin rejection")
	}
	if got := err.Error(); strings.Contains(got, secret) {
		t.Fatalf("Run() error leaked stdin: %q", got)
	}
}

func TestBoundedBufferDiagnosticRetentionCapacity(t *testing.T) {
	for _, limit := range []int{4096, 32768} {
		buffer := newBoundedBuffer(limit)
		for i := 0; i < limit/333+2; i++ {
			buffer.Write([]byte(strings.Repeat("m", 333)))
			if len(buffer.contents) > limit || cap(buffer.contents) > limit {
				t.Fatal("drain allocation exceeded fixed retention", limit, len(buffer.contents), cap(buffer.contents))
			}
		}
		if len(buffer.String()) != limit || !buffer.Truncated() {
			t.Fatal("overrun retention/truncation")
		}
	}
}

func TestOSRunnerDiagnosticStreamChild(t *testing.T) {
	if len(os.Args) != 4 || os.Args[2] != "--" {
		return
	}
	fmt.Fprint(os.Stdout, "abcdef")
	fmt.Fprint(os.Stderr, "uvwxyz")
	os.Exit(0)
}
func TestOSRunnerDiagnosticCapsPreserveOutputFallback(t *testing.T) {
	for _, test := range []struct {
		name      string
		runner    OSRunner
		out, err  string
		truncated bool
	}{{"default", OSRunner{}, "abcdef", "uvwxyz", false}, {"legacy", OSRunner{MaxOutputBytes: 3}, "abc", "uvw", true}, {"distinct", OSRunner{MaxOutputBytes: 3, MaxStdoutBytes: 4, MaxStderrBytes: 2}, "abcd", "uv", true}} {
		t.Run(test.name, func(t *testing.T) {
			result, err := test.runner.Run(t.Context(), Command{Path: os.Args[0], Args: []string{"-test.run=^TestOSRunnerDiagnosticStreamChild$", "--", "fixed"}, Env: []string{"LANG=C"}})
			if err != nil || result.Stdout != test.out || result.Stderr != test.err || result.Truncated != test.truncated {
				t.Fatal("per-stream cap/fallback changed", err)
			}
		})
	}
}

func TestOSRunnerDiagnosticStrictStderrObservedEOF(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	runner := OSRunner{MaxStdoutBytes: 512, MaxStderrBytes: 512}
	value := reflect.ValueOf(&runner).Elem().FieldByName("StrictStderr")
	if value.IsValid() {
		value.SetBool(true)
	}
	result, err := runner.Run(ctx, Command{Path: os.Args[0], Args: []string{"-test.run=^TestOSRunnerDiagnosticStreamChild$", "--", "fixed"}, Env: []string{"LANG=C", "GORACE=atexit_sleep_ms=0"}})
	if err != nil || result.Stdout != "abcdef" || result.Stderr != "uvwxyz" {
		t.Fatal("original child exchange changed", err)
	}
	complete := reflect.ValueOf(result).FieldByName("StderrComplete")
	if !complete.IsValid() || !complete.Bool() {
		t.Fatal("actual stderr EOF/read-close observation unavailable")
	}
}
