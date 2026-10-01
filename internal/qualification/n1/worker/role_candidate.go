//go:build n1diagnostic && n1clipboarddiagnostic && !n1candidate

package worker

import (
	"context"

	"github.com/weshofmann/boxwarden/internal/config"
	"github.com/weshofmann/boxwarden/internal/networkdiag"
	"github.com/weshofmann/boxwarden/internal/supervisor"
)

const role = "candidate"
const roleName = config.N1CandidateName

func (w *Worker) observeLaunch(ctx context.Context, b supervisor.Binding, n networkdiag.Inspection) (*Launch, error) {
	r, ok := w.reader.(interface {
		ObserveDiagnosticLaunch(context.Context, supervisor.Binding) (networkdiag.LaunchObservation, error)
	})
	if !ok {
		return nil, ErrRefused
	}
	l, e := r.ObserveDiagnosticLaunch(ctx, b)
	if e != nil || !l.Valid() || l.Inspection.Binding != n.Binding || l.Inspection.PinFingerprint != n.PinFingerprint || l.Inspection.ConfigSHA256 != n.ConfigSHA256 {
		return nil, ErrRefused
	}
	return &Launch{Nonce: l.Watch.Hello.Nonce, Gateway: l.Watch.Hello.Gateway, Owner: Process{l.Owner.PID, l.Owner.BirthUS, l.Owner.UniqueID}, Child: Process{l.Child.PID, l.Child.BirthUS, l.Child.UniqueID}, Anchor: l.Watch.Anchor, Deadline: l.Watch.Deadline, Observed: l.Watch.Observed}, nil
}
func (w *Worker) recheckLaunch(ctx context.Context, b supervisor.Binding, n networkdiag.Inspection, previous *Launch) error {
	next, e := w.observeLaunch(ctx, b, n)
	if e != nil || previous == nil || next.Nonce != previous.Nonce || next.Gateway != previous.Gateway || next.Owner != previous.Owner || next.Child != previous.Child || next.Anchor != previous.Anchor || next.Deadline != previous.Deadline || next.Observed.WallNS < previous.Observed.WallNS || next.Observed.ContinuousNS < previous.Observed.ContinuousNS {
		return ErrRefused
	}
	previous.Observed = next.Observed
	return nil
}
func (w *Worker) Arm(ctx context.Context, id Identity, a networkdiag.Arm) (networkdiag.ArmReceipt, error) {
	if _, e := w.Inspect(ctx, id); e != nil {
		return networkdiag.ArmReceipt{}, e
	}
	r := &supervisor.Client{RuntimeDirectory: w.domain.StateRoot + "/runtime/n1qualification/" + id.Session + "/" + id.Generation}
	return r.ArmDiagnosticWatchReceipt(ctx, binding(id), a)
}
func (w *Worker) Collect(ctx context.Context, id Identity, op string) (networkdiag.Summary, error) {
	if !contractUUID(op) {
		return networkdiag.Summary{}, ErrRefused
	}
	if _, e := w.inspect(ctx, id, false); e != nil {
		return networkdiag.Summary{}, e
	}
	r := &supervisor.Client{RuntimeDirectory: w.domain.StateRoot + "/runtime/n1qualification/" + id.Session + "/" + id.Generation}
	return r.CollectDiagnosticWatch(ctx, binding(id), op)
}
func contractUUID(s string) bool { return networkdiag.UUID(s) }
