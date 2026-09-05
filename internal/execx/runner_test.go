package execx

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
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
