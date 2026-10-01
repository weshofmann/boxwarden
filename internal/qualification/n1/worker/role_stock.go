//go:build n1clipboarddiagnostic && !n1diagnostic && !n1candidate

package worker

import (
	"context"

	"github.com/weshofmann/boxwarden/internal/config"
	"github.com/weshofmann/boxwarden/internal/networkdiag"
	"github.com/weshofmann/boxwarden/internal/supervisor"
)

const role = "control"
const roleName = config.N1ControlName

func (w *Worker) observeLaunch(context.Context, supervisor.Binding, networkdiag.Inspection) (*Launch, error) {
	return nil, nil
}
func (w *Worker) recheckLaunch(context.Context, supervisor.Binding, networkdiag.Inspection, *Launch) error {
	return nil
}
func (w *Worker) Arm(context.Context, Identity, networkdiag.Arm) (networkdiag.ArmReceipt, error) {
	return networkdiag.ArmReceipt{}, ErrRefused
}
func (w *Worker) Collect(context.Context, Identity, string) (networkdiag.Summary, error) {
	return networkdiag.Summary{}, ErrRefused
}
