package supervisor

import (
	"context"
	"fmt"
	"time"

	"github.com/weshofmann/boxwarden/internal/guestproto"
)

const exactActionSnapshotAge = 5 * time.Second

// ExactActionController can observe and run only an already live, exact
// generation below its configured runtime root. It cannot launch or stop VMs.
type ExactActionController struct{ runtimeRoot string }

func NewExactActionController(runtimeRoot string) (*ExactActionController, error) {
	if !canonicalAbsolute(runtimeRoot) {
		return nil, fmt.Errorf("exact action runtime root is required")
	}
	return &ExactActionController{runtimeRoot: runtimeRoot}, nil
}

func (c *ExactActionController) Snapshot(ctx context.Context, binding Binding) (Snapshot, error) {
	if c == nil {
		return Snapshot{}, fmt.Errorf("exact action controller is required")
	}
	directory, err := exactRuntimeDirectory(c.runtimeRoot, binding)
	if err != nil {
		return Snapshot{}, err
	}
	return (&Client{RuntimeDirectory: directory, MaxSnapshotAge: exactActionSnapshotAge}).Snapshot(ctx, binding)
}

func (c *ExactActionController) RunAction(ctx context.Context, binding Binding, request guestproto.ActionRequest) (guestproto.ActionReceipt, error) {
	if c == nil {
		return guestproto.ActionReceipt{}, fmt.Errorf("exact action controller is required")
	}
	directory, err := exactRuntimeDirectory(c.runtimeRoot, binding)
	if err != nil {
		return guestproto.ActionReceipt{}, err
	}
	return (&Client{RuntimeDirectory: directory, MaxSnapshotAge: exactActionSnapshotAge}).RunAction(ctx, binding, request)
}
