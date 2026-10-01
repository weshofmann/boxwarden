//go:build n1clipboarddiagnostic && !n1candidate

package worker

import (
	"context"
	"net"

	"github.com/weshofmann/boxwarden/internal/config"
	"github.com/weshofmann/boxwarden/internal/networkdiag"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/contract"
	"github.com/weshofmann/boxwarden/internal/session"
	"github.com/weshofmann/boxwarden/internal/sshx"
	"github.com/weshofmann/boxwarden/internal/supervisor"
)

func (w *Worker) Controls(ctx context.Context, id Identity) (sshx.N1ControlsResult, error) {
	o, e := w.inspect(ctx, id, false)
	if e != nil {
		return sshx.N1ControlsResult{}, e
	}
	return w.guest.RunN1Controls(ctx, o.Connection, guestBinding(id))
}
func (w *Worker) Positive(ctx context.Context, id Identity) error {
	o, e := w.inspect(ctx, id, false)
	if e != nil {
		return e
	}
	p, e := w.guest.Probe(ctx, o.Connection, sshx.ProbeRequest{})
	if e != nil || !p.OK {
		return ErrRefused
	}
	_, e = w.inspect(ctx, id, false)
	return e
}
func matchesPeer(p sshx.N1Peer, n networkdiag.Binding) bool {
	return p.SessionUUID == n.SessionID && p.GenerationUUID == n.Generation && p.Backend == n.BackendObject && p.Address == net.IP(n.Address[:]).String() && p.MAC == net.HardwareAddr(n.MAC[:]).String()
}
func (w *Worker) admitOther(ctx context.Context, p sshx.N1Peer) error {
	name := config.N1ControlName
	if role == "control" {
		name = config.N1CandidateName
	}
	r, e := session.LoadRecord(w.domain.StateRoot, config.N1Domain, name)
	if e != nil || r.ID != p.SessionUUID || r.StartGeneration != p.GenerationUUID || r.Backend.Kind != "tart" || r.Backend.ObjectID != p.Backend || r.Mode != session.ModeQuarantine || r.GoldenRevision != contract.BaseName || r.IntendedState != session.StateRunning || r.RecipeIntentDigest != "" {
		return ErrRefused
	}
	b := supervisor.Binding{Domain: config.N1Domain, SessionID: r.ID, BackendKind: "tart", BackendObject: r.Backend.ObjectID, Generation: r.StartGeneration}
	s, e := w.reader.Snapshot(ctx, b)
	if e != nil || !ready(s, b) {
		return ErrRefused
	}
	n, e := w.reader.InspectDiagnosticNetwork(ctx, b)
	if e != nil || !n.Valid() || !matchesPeer(p, n.Binding) {
		return ErrRefused
	}
	s, e = w.reader.Snapshot(ctx, b)
	if e != nil || !ready(s, b) {
		return ErrRefused
	}
	after, e := w.reader.InspectDiagnosticNetwork(ctx, b)
	if e != nil || !after.Valid() || after.Binding != n.Binding || after.PinFingerprint != n.PinFingerprint || after.ConfigSHA256 != n.ConfigSHA256 || !matchesPeer(p, after.Binding) {
		return ErrRefused
	}
	final, e := session.LoadRecord(w.domain.StateRoot, config.N1Domain, name)
	if e != nil || final != r {
		return ErrRefused
	}
	return nil
}
func (w *Worker) ObservePeer(ctx context.Context, id Identity, watch sshx.N1PeerWatch, onReady func(sshx.N1PeerReady) error) (sshx.N1PeerSummary, error) {
	o, e := w.Inspect(ctx, id)
	if e != nil {
		return sshx.N1PeerSummary{}, e
	}
	self, other := watch.Candidate, watch.Control
	if role == "control" {
		self, other = other, self
	}
	if watch.Role != role || watch.Domain != config.N1Domain || watch.DurationMS != 30000 || !matchesPeer(self, o.Network.Binding) || w.admitOther(ctx, other) != nil {
		return sshx.N1PeerSummary{}, ErrRefused
	}
	return w.guest.ObserveN1Peer(ctx, o.Connection, watch, onReady)
}

// Peer is independently resolved against the fixed opposite-role record. The
// public connect method cannot target an arbitrary private endpoint.
func (w *Worker) Connect(ctx context.Context, id Identity, peer sshx.N1Peer) (sshx.N1ConnectResult, error) {
	if role != "candidate" || w.admitOther(ctx, peer) != nil {
		return sshx.N1ConnectResult{}, ErrRefused
	}
	o, e := w.inspect(ctx, id, false)
	if e != nil {
		return sshx.N1ConnectResult{}, e
	}
	return w.guest.ConnectN1Peer(ctx, o.Connection, sshx.N1ConnectRequest{Binding: guestBinding(id), ControlIPv4: peer.Address})
}
