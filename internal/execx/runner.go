package execx

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
)

const (
	defaultMaxOutputBytes = 64 << 10
	defaultMaxStdinBytes  = 64 << 10
)

type Command struct {
	Path string
	Args []string
	Env  []string
	// Stdin is passed directly to the child process. It is never included in
	// Result or diagnostics because callers use it for credential-bearing frames.
	Stdin []byte
}

type Result struct {
	Stdout    string
	Stderr    string
	Truncated bool
	// StderrComplete is opt-in actual EOF/read-close evidence, never exit status.
	StderrComplete                   bool
	StdoutTruncated, StderrTruncated bool
}

type Runner interface {
	Run(context.Context, Command) (Result, error)
}

type OSRunner struct {
	MaxOutputBytes int
	// Positive per-stream retention overrides; otherwise each stream uses the
	// existing MaxOutputBytes/default fallback. Commands cannot choose limits.
	MaxStdoutBytes int
	MaxStderrBytes int
	MaxStdinBytes  int
	StrictStderr   bool
}

func (r OSRunner) Run(ctx context.Context, command Command) (Result, error) {
	if command.Path == "" {
		return Result{}, fmt.Errorf("command path is required")
	}
	if isShell(command.Path) {
		return Result{}, fmt.Errorf("shell command %q is prohibited", command.Path)
	}
	stdinLimit := r.MaxStdinBytes
	if stdinLimit <= 0 {
		stdinLimit = defaultMaxStdinBytes
	}
	if len(command.Stdin) > stdinLimit {
		return Result{}, fmt.Errorf("command stdin exceeds %d-byte limit", stdinLimit)
	}

	process := exec.CommandContext(ctx, command.Path, command.Args...)
	if command.Env != nil {
		process.Env = command.Env
	}
	if command.Stdin != nil {
		process.Stdin = bytes.NewReader(command.Stdin)
	}
	limit := r.MaxOutputBytes
	if limit <= 0 {
		limit = defaultMaxOutputBytes
	}
	stdoutLimit, stderrLimit := r.MaxStdoutBytes, r.MaxStderrBytes
	if stdoutLimit <= 0 {
		stdoutLimit = limit
	}
	if stderrLimit <= 0 {
		stderrLimit = limit
	}
	stdout := newBoundedBuffer(stdoutLimit)
	stderr := newBoundedBuffer(stderrLimit)
	process.Stdout = stdout
	process.Stderr = stderr
	var err error
	observed := false
	if r.StrictStderr {
		err, observed = runStrictStderr(ctx, process, stderr, func(f *os.File) error { return f.Close() })
	} else {
		err = process.Run()
	}
	result := Result{Stdout: stdout.String(), Stderr: stderr.String(), Truncated: stdout.Truncated() || stderr.Truncated(), StderrComplete: observed, StdoutTruncated: stdout.Truncated(), StderrTruncated: stderr.Truncated()}
	if err != nil {
		return result, fmt.Errorf("run %q: %w", command.Path, err)
	}
	return result, nil
}

func isShell(path string) bool {
	switch filepath.Base(path) {
	case "sh", "bash", "dash", "zsh", "fish":
		return true
	default:
		return false
	}
}

type boundedBuffer struct {
	mu        sync.Mutex
	limit     int
	contents  []byte
	truncated bool
}

func newBoundedBuffer(limit int) *boundedBuffer {
	return &boundedBuffer{limit: limit}
}

func (b *boundedBuffer) Write(input []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	remaining := b.limit - len(b.contents)
	if remaining <= 0 {
		b.truncated = true
		return len(input), nil
	}
	retained := input
	if len(retained) > remaining {
		retained = retained[:remaining]
		b.truncated = true
	}
	needed := len(b.contents) + len(retained)
	if needed > cap(b.contents) {
		capacity := cap(b.contents) * 2
		if capacity < needed {
			capacity = needed
		}
		if capacity > b.limit {
			capacity = b.limit
		}
		grown := make([]byte, len(b.contents), capacity)
		copy(grown, b.contents)
		b.contents = grown
	}
	b.contents = append(b.contents, retained...)
	return len(input), nil
}

func (b *boundedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(b.contents)
}

func (b *boundedBuffer) Truncated() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.truncated
}
