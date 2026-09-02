package tart

import (
	"context"
	"fmt"
	"strings"

	"github.com/weshofmann/boxwarden/internal/backend"
	"github.com/weshofmann/boxwarden/internal/execx"
)

// Clone makes a copy-on-write Tart object using separately validated argv
// operands.
func (o Observer) Clone(ctx context.Context, sourceID, targetID string) error {
	if err := backend.ValidateObjectID(sourceID); err != nil {
		return fmt.Errorf("clone Tart object: invalid source ID: %w", err)
	}
	if err := backend.ValidateObjectID(targetID); err != nil {
		return fmt.Errorf("clone Tart object: invalid target ID: %w", err)
	}
	return o.runCreateCommand(ctx, "clone Tart object", []string{"clone", sourceID, targetID})
}

// RandomizeMAC replaces a Tart object's MAC address before its first boot.
func (o Observer) RandomizeMAC(ctx context.Context, objectID string) error {
	if err := backend.ValidateObjectID(objectID); err != nil {
		return fmt.Errorf("randomize Tart object MAC: invalid object ID: %w", err)
	}
	return o.runCreateCommand(ctx, "randomize Tart object MAC", []string{"set", objectID, "--random-mac"})
}

func (o Observer) runCreateCommand(ctx context.Context, operation string, args []string) error {
	if o.runner == nil {
		return fmt.Errorf("%s: runner is required", operation)
	}
	if strings.TrimSpace(o.executable) == "" {
		return fmt.Errorf("%s: executable is required", operation)
	}
	timeout := mutationCommandTimeout
	if len(args) > 0 && args[0] == "clone" {
		timeout = cloneCommandTimeout
	}
	commandContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	result, err := o.runner.Run(commandContext, execx.Command{Path: o.executable, Args: args})
	if err != nil {
		return fmt.Errorf("%s: %w", operation, err)
	}
	if result.Truncated {
		return fmt.Errorf("%s: Tart output exceeded the trusted-host limit", operation)
	}
	return nil
}

var _ backend.Creator = Observer{}
