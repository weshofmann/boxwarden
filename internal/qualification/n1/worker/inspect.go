//go:build n1clipboarddiagnostic && !n1candidate

package worker

import (
	"bytes"
	"context"
	"net"
	"time"

	"github.com/weshofmann/boxwarden/internal/qualification/n1/contract"
	"github.com/weshofmann/boxwarden/internal/session"
	"github.com/weshofmann/boxwarden/internal/sshx"
)

func (w *Worker) Inspect(ctx context.Context, id Identity) (RuntimeObservation, error) {
	return w.inspect(ctx, id, true)
}
func (w *Worker) inspect(ctx context.Context, id Identity, unarmed bool) (RuntimeObservation, error) {
	refuse := func() (RuntimeObservation, error) { return RuntimeObservation{}, ErrRefused }
	if w == nil || w.reader == nil || ctx.Err() != nil {
		return refuse()
	}
	r, e := session.LoadRecord(w.domain.StateRoot, configDomain(), roleName)
	if e != nil || admitRecord(r, id) != nil {
		return refuse()
	}
	b := binding(id)
	before, e := w.reader.Snapshot(ctx, b)
	if e != nil || !ready(before, b) {
		return refuse()
	}
	n, e := w.reader.InspectDiagnosticNetwork(ctx, b)
	if e != nil || !n.Valid() || n.Binding.Domain != b.Domain || n.Binding.SessionID != b.SessionID || n.Binding.Generation != b.Generation || n.Binding.BackendObject != b.BackendObject {
		return refuse()
	}
	runtime := w.domain.StateRoot + "/runtime/" + b.Domain + "/" + id.Session + "/" + id.Generation
	sb := sshx.Binding{Domain: w.domain.ID, SessionID: id.Session, BackendKind: "tart", BackendObject: id.Backend}
	pin, e := sshx.NewPinStore(sshx.Domain{ID: w.domain.ID, StateRoot: w.domain.StateRoot}).Load(ctx, sb)
	if e != nil || pin.Fingerprint != n.PinFingerprint {
		return refuse()
	}
	pinRaw, _, e := readLeaf(w.domain.StateRoot+"/identity/ssh-host-pins/"+id.Session+".json", 0600, 4096)
	if e != nil {
		return refuse()
	}
	peer := contract.Peer{Session: id.Session, Backend: id.Backend, HostPinSHA: contract.SHA(pinRaw)}
	if _, e = contract.ParseHostPin(pinRaw, peer); e != nil {
		return refuse()
	}
	caRaw, _, e := readLeaf(w.domain.StateRoot+"/identity/ssh-user-ca/metadata.json", 0600, 4096)
	if e != nil {
		return refuse()
	}
	ca, e := contract.ParseCAPublic(caRaw)
	if e != nil {
		return refuse()
	}
	caKey, _, e := readLeaf(w.domain.StateRoot+"/identity/ssh-user-ca/ca.pub", 0644, 4096)
	if e != nil || contract.MatchPublicKey(caKey, ca) != nil {
		return refuse()
	}
	conn := sshx.Connection{Address: net.IP(n.Binding.Address[:]).String(), Port: 22, Binding: sb, Pin: pin, RuntimeDirectory: runtime, IdentityFile: runtime + "/client", CertificateFile: runtime + "/client-cert.pub", KnownHostsFile: runtime + "/known_hosts"}
	key, e := metadata(conn.IdentityFile, 0600)
	if e != nil {
		return refuse()
	}
	cert, e := sshx.InspectN1Certificate(conn, ca.PublicKey, time.Now())
	if e != nil || cert.CAFingerprint != ca.Fingerprint {
		return refuse()
	}
	var launch *Launch
	if unarmed {
		launch, e = w.observeLaunch(ctx, b, n)
		if e != nil {
			return refuse()
		}
	}
	after, e := w.reader.Snapshot(ctx, b)
	if e != nil || !ready(after, b) {
		return refuse()
	}
	after.Diagnostic = ""
	final, e := session.LoadRecord(w.domain.StateRoot, configDomain(), roleName)
	if e != nil || final != r || admitRecord(final, id) != nil {
		return refuse()
	}
	keyAfter, e := metadata(conn.IdentityFile, 0600)
	if e != nil || key != keyAfter {
		return refuse()
	}
	certAfter, e := sshx.InspectN1Certificate(conn, ca.PublicKey, time.Now())
	if e != nil || certAfter != cert {
		return refuse()
	}
	pinAfter, e := sshx.NewPinStore(sshx.Domain{ID: w.domain.ID, StateRoot: w.domain.StateRoot}).Load(ctx, sb)
	if e != nil || pinAfter != pin {
		return refuse()
	}
	nAfter, e := w.reader.InspectDiagnosticNetwork(ctx, b)
	if e != nil || nAfter.Binding != n.Binding || nAfter.PinFingerprint != n.PinFingerprint || nAfter.ConfigSHA256 != n.ConfigSHA256 {
		return refuse()
	}
	if unarmed {
		if e = w.recheckLaunch(ctx, b, nAfter, launch); e != nil {
			return refuse()
		}
	}
	caFinal, _, e := readLeaf(w.domain.StateRoot+"/identity/ssh-user-ca/metadata.json", 0600, 4096)
	if e != nil || contract.SHA(caFinal) != contract.SHA(caRaw) {
		return refuse()
	}
	caKeyFinal, _, e := readLeaf(w.domain.StateRoot+"/identity/ssh-user-ca/ca.pub", 0644, 4096)
	if e != nil || !bytes.Equal(caKey, caKeyFinal) || contract.MatchPublicKey(caKeyFinal, ca) != nil {
		return refuse()
	}
	pinFinal, _, e := readLeaf(w.domain.StateRoot+"/identity/ssh-host-pins/"+id.Session+".json", 0600, 4096)
	if e != nil || contract.SHA(pinFinal) != peer.HostPinSHA {
		return refuse()
	}
	result := RuntimeObservation{Version: 1, Role: role, Name: roleName, Identity: id, Ready: after, Network: nAfter, Connection: conn, Certificate: cert, PrivateKey: key, HostPinSHA: peer.HostPinSHA, CASHA: contract.SHA(caRaw), Launch: launch}
	if _, e = bounded(result, contract.MaxReceiptBytes); e != nil || ctx.Err() != nil {
		return refuse()
	}
	return result, nil
}
func configDomain() string { return "n1qualification" }
