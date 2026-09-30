//go:build n1diagnostic && !n1candidate

package sessionruntime

import (
	"context"
	"github.com/weshofmann/boxwarden/internal/config"
	"github.com/weshofmann/boxwarden/internal/networkdiag"
	"github.com/weshofmann/boxwarden/internal/session"
	"github.com/weshofmann/boxwarden/internal/supervisor"
	"path/filepath"
)

func (o *Owner) diagnosticWatch() *networkdiag.Watch {
	if o == nil {
		return nil
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if h, ok := o.handle.(interface{ DiagnosticWatch() *networkdiag.Watch }); ok {
		return h.DiagnosticWatch()
	}
	return nil
}
func (o *Owner) ArmDiagnosticWatch(ctx context.Context, a networkdiag.Arm) (networkdiag.Armed, error) {
	w := o.diagnosticWatch()
	if w == nil {
		return networkdiag.Armed{}, networkdiag.ErrMetadata
	}
	o.diagnostic.mu.Lock()
	if o.diagnostic.attempted {
		o.diagnostic.mu.Unlock()
		w.Invalidate()
		return networkdiag.Armed{}, networkdiag.ErrMetadata
	}
	o.diagnostic.attempted = true
	o.diagnostic.arm = a
	o.diagnostic.mu.Unlock()
	pair, err := o.currentDiagnosticPair(ctx, a)
	if err != nil {
		w.Invalidate()
		return networkdiag.Armed{}, networkdiag.ErrMetadata
	}
	armed, err := w.Arm(ctx, a)
	if err != nil {
		return networkdiag.Armed{}, err
	}
	o.diagnostic.mu.Lock()
	o.diagnostic.before = pair
	o.diagnostic.armed = armed
	o.diagnostic.mu.Unlock()
	return armed, nil
}
func (o *Owner) CollectDiagnosticWatch(ctx context.Context, operation string) (networkdiag.Summary, error) {
	w := o.diagnosticWatch()
	if w == nil {
		return networkdiag.Summary{}, networkdiag.ErrMetadata
	}
	o.diagnostic.mu.Lock()
	a, before, armed := o.diagnostic.arm, o.diagnostic.before, o.diagnostic.armed
	o.diagnostic.mu.Unlock()
	if operation != a.OperationID || !armed.Matches(a) {
		return networkdiag.Summary{}, networkdiag.ErrMetadata
	}
	summary, err := w.Collect(ctx, operation)
	if err != nil {
		return networkdiag.Summary{}, err
	}
	after, err := o.currentDiagnosticPair(ctx, a)
	if err != nil || !sameDiagnosticPair(before, after) || !summary.Matches(a, armed) {
		w.Invalidate()
		return networkdiag.Summary{}, networkdiag.ErrMetadata
	}
	return summary, nil
}
func sameDiagnosticPair(a, b [2]networkdiag.Inspection) bool {
	for i := range a {
		if !a[i].Valid() || !b[i].Valid() || a[i].Binding != b[i].Binding || a[i].PinFingerprint != b[i].PinFingerprint || a[i].ConfigSHA256 != b[i].ConfigSHA256 {
			return false
		}
	}
	return true
}
func diagnosticRecordMatches(r session.Record, name string, b networkdiag.Binding) bool {
	return string(r.Domain) == config.N1Domain && string(r.Name) == name && r.ID == b.SessionID && r.StartGeneration == b.Generation && r.Backend.Kind == b.BackendKind && r.Backend.ObjectID == b.BackendObject && r.IntendedState == session.StateRunning && r.Readiness.Status == session.ReadinessReady
}
func (o *Owner) currentDiagnosticPair(ctx context.Context, a networkdiag.Arm) ([2]networkdiag.Inspection, error) {
	if o.deps.diagnostic.pair != nil {
		return o.deps.diagnostic.pair(ctx, a)
	} // private synthetic composition only
	var result [2]networkdiag.Inspection
	if !a.Valid(a.Generation, a.Nonce, a.Candidate.MAC, a.Gateway) || ctx.Err() != nil {
		return result, networkdiag.ErrMetadata
	}
	ownConfig, err := config.LoadN1CurrentEnrollment()
	if err != nil {
		return result, err
	}
	ownDomain, err := ownConfig.Domain(config.N1Domain)
	if err != nil {
		return result, err
	}
	own, err := session.LoadRecord(ownDomain.StateRoot, config.N1Domain, config.N1CandidateName)
	if err != nil || !diagnosticRecordMatches(own, config.N1CandidateName, a.Candidate) {
		return result, networkdiag.ErrMetadata
	}
	peerConfig, err := config.LoadN1ControlEnrollment()
	if err != nil {
		return result, err
	}
	peerDomain, err := peerConfig.Domain(config.N1Domain)
	if err != nil {
		return result, err
	}
	peer, err := session.LoadRecord(peerDomain.StateRoot, config.N1Domain, config.N1ControlName)
	if err != nil || !diagnosticRecordMatches(peer, config.N1ControlName, a.Control) || session.RequireNoRebuild(peerDomain.StateRoot, peerDomain.ID, config.N1ControlName) != nil {
		return result, networkdiag.ErrMetadata
	}
	reader, err := supervisor.NewExactSnapshotReader(filepath.Join(peerDomain.StateRoot, "runtime"))
	if err != nil {
		return result, err
	}
	result[0], err = o.InspectDiagnosticNetwork(ctx)
	if err != nil || result[0].Binding != a.Candidate {
		return result, networkdiag.ErrMetadata
	}
	result[1], err = reader.InspectDiagnosticNetwork(ctx, supervisor.Binding{Domain: a.Control.Domain, SessionID: a.Control.SessionID, Generation: a.Control.Generation, BackendKind: a.Control.BackendKind, BackendObject: a.Control.BackendObject})
	if err != nil || result[1].Binding != a.Control {
		return result, networkdiag.ErrMetadata
	}
	ownAfter, e1 := session.LoadRecord(ownDomain.StateRoot, config.N1Domain, config.N1CandidateName)
	peerAfter, e2 := session.LoadRecord(peerDomain.StateRoot, config.N1Domain, config.N1ControlName)
	if e1 != nil || e2 != nil || ownAfter != own || peerAfter != peer || ctx.Err() != nil {
		return result, networkdiag.ErrMetadata
	}
	if _, err = config.LoadN1CurrentEnrollment(); err != nil {
		return result, err
	}
	if _, err = config.LoadN1ControlEnrollment(); err != nil {
		return result, err
	}
	return result, nil
}
