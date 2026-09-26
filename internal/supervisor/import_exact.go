package supervisor

import (
	"context"
	"fmt"
	"time"
)

const exactImportSnapshotAge = 5 * time.Second

// ExactImportController can observe and transfer only through an already live
// generation below one configured runtime root. It cannot launch or stop VMs.
type ExactImportController struct{ runtimeRoot string }

func NewExactImportController(runtimeRoot string) (*ExactImportController, error) {
	if !canonicalAbsolute(runtimeRoot) {
		return nil, fmt.Errorf("exact import runtime root is required")
	}
	return &ExactImportController{runtimeRoot: runtimeRoot}, nil
}

func (c *ExactImportController) Snapshot(ctx context.Context, binding Binding) (Snapshot, error) {
	if c == nil {
		return Snapshot{}, fmt.Errorf("exact import controller is required")
	}
	directory, err := exactRuntimeDirectory(c.runtimeRoot, binding)
	if err != nil {
		return Snapshot{}, err
	}
	return (&Client{RuntimeDirectory: directory, MaxSnapshotAge: exactImportSnapshotAge}).Snapshot(ctx, binding)
}

func (c *ExactImportController) TransferImport(ctx context.Context, binding Binding, spec ImportTransfer) (ImportResult, error) {
	if c == nil {
		return ImportResult{}, fmt.Errorf("exact import controller is required")
	}
	directory, err := exactRuntimeDirectory(c.runtimeRoot, binding)
	if err != nil {
		return ImportResult{}, err
	}
	return (&Client{RuntimeDirectory: directory, MaxSnapshotAge: exactImportSnapshotAge}).TransferImport(ctx, binding, spec)
}
