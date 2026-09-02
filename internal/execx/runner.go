package execx

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"sync"
)

const defaultMaxOutputBytes = 64 << 10

type Command struct {
	Path string
	Args []string
	Env  []string
}

type Result struct {
	Stdout    string
	Stderr    string
	Truncated bool
}

type Runner interface {
	Run(context.Context, Command) (Result, error)
}

type OSRunner struct {
	MaxOutputBytes int
}

func (r OSRunner) Run(ctx context.Context, command Command) (Result, error) {
	if command.Path == "" {
		return Result{}, fmt.Errorf("command path is required")
	}
	if isShell(command.Path) {
		return Result{}, fmt.Errorf("shell command %q is prohibited", command.Path)
	}

	process := exec.CommandContext(ctx, command.Path, command.Args...)
	if command.Env != nil {
		process.Env = command.Env
	}
	limit := r.MaxOutputBytes
	if limit <= 0 {
		limit = defaultMaxOutputBytes
	}
	stdout := newBoundedBuffer(limit)
	stderr := newBoundedBuffer(limit)
	process.Stdout = stdout
	process.Stderr = stderr

	err := process.Run()
	result := Result{Stdout: stdout.String(), Stderr: stderr.String(), Truncated: stdout.Truncated() || stderr.Truncated()}
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
	if len(input) > remaining {
		b.contents = append(b.contents, input[:remaining]...)
		b.truncated = true
		return len(input), nil
	}
	b.contents = append(b.contents, input...)
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
