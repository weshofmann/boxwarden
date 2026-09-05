package tart

import (
	"context"
	"fmt"
	"time"

	"github.com/weshofmann/boxwarden/internal/backend"
	"github.com/weshofmann/boxwarden/internal/execx"
)

const deleteCommandLimit = time.Minute

// Deleter executes Tart's exact deletion operation for one validated object.
type Deleter struct {
	runner   execx.Runner
	tartPath string
	tartHome string
}

func NewDeleter(runner execx.Runner, tartPath, tartHome string) Deleter {
	return Deleter{runner: runner, tartPath: tartPath, tartHome: tartHome}
}

func (d Deleter) Delete(ctx context.Context, objectID string) error {
	if err := backend.ValidateObjectID(objectID); err != nil {
		return fmt.Errorf("delete Tart object: %w", err)
	}
	if d.runner == nil || !canonicalAbsolutePath(d.tartPath) || !canonicalAbsolutePath(d.tartHome) {
		return fmt.Errorf("delete Tart object: qualified runner, Tart path, and Tart home are required")
	}
	commandContext, cancel := context.WithTimeout(ctx, deleteCommandLimit)
	defer cancel()
	result, err := d.runner.Run(commandContext, execx.Command{
		Path: d.tartPath,
		Args: []string{"delete", objectID},
		Env:  tartCommandEnvironment(d.tartHome),
	})
	if err != nil {
		return fmt.Errorf("delete Tart object: %w", err)
	}
	if result.Truncated {
		return fmt.Errorf("delete Tart object: output exceeded trusted-host limit")
	}
	return nil
}

func tartCommandEnvironment(tartHome string) []string {
	return []string{"TART_HOME=" + tartHome, "LANG=C", "LC_ALL=C"}
}

var _ backend.Deleter = Deleter{}
