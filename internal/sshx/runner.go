package sshx

import (
	"context"
	"fmt"

	"github.com/weshofmann/boxwarden/internal/execx"
)

// ExecRunner is the production argv-only SSH runner. ssh-keygen inspection is
// text-formatted, so it runs with a closed UTC/C environment.
type ExecRunner struct{ runner execx.Runner }

func NewExecRunner() ExecRunner {
	return newExecRunner(execx.OSRunner{MaxOutputBytes: maxStateFileBytes})
}

func newExecRunner(runner execx.Runner) ExecRunner { return ExecRunner{runner: runner} }

func (r ExecRunner) Run(ctx context.Context, command Command) (Result, error) {
	if r.runner == nil {
		return Result{}, fmt.Errorf("SSH exec runner dependency is required")
	}
	result, err := r.runner.Run(ctx, execx.Command{
		Path: command.Path, Args: append([]string(nil), command.Args...), Stdin: command.Stdin,
		Env: []string{"LC_ALL=C", "LANG=C", "TZ=UTC"},
	})
	return Result{Stdout: result.Stdout, Stderr: result.Stderr, Truncated: result.Truncated}, err
}
