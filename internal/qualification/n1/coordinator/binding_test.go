//go:build n1diagnostic && n1clipboarddiagnostic && !n1candidate

package coordinator

import (
	"encoding/json"
	"github.com/weshofmann/boxwarden/internal/clipboarddiag"
	"github.com/weshofmann/boxwarden/internal/config"
	"github.com/weshofmann/boxwarden/internal/networkdiag"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/clock"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/contract"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/worker"
	"github.com/weshofmann/boxwarden/internal/sshx"
	"github.com/weshofmann/boxwarden/internal/supervisor"
	"strings"
	"testing"
	"time"
)

func runtimeFixture() worker.RuntimeObservation {
	id := worker.Identity{Session: "20000000-0000-4000-8000-000000000001", Generation: "30000000-0000-4000-8000-000000000001"}
	id.Backend = "boxwarden-n1qualification-" + strings.ReplaceAll(id.Session, "-", "")
	b := supervisor.Binding{Domain: config.N1Domain, SessionID: id.Session, BackendKind: "tart", BackendObject: id.Backend, Generation: id.Generation}
	sb := sshx.Binding{Domain: config.N1Domain, SessionID: id.Session, BackendKind: "tart", BackendObject: id.Backend}
	r := contract.StateRoot + "/runtime/n1qualification/" + id.Session + "/" + id.Generation
	n := networkdiag.Inspection{Binding: networkdiag.Binding{Domain: config.N1Domain, SessionID: id.Session, Generation: id.Generation, BackendKind: "tart", BackendObject: id.Backend, Address: [4]uint8{192, 168, 64, 2}, MAC: [6]uint8{2, 0, 0, 0, 0, 2}}, PinFingerprint: "SHA256:" + strings.Repeat("a", 43), ConfigSHA256: contract.StockConfigSHA, ObservedUnixNS: 1}
	now := uint64(time.Now().Unix()) - 1
	return worker.RuntimeObservation{Version: 1, Role: "control", Name: config.N1ControlName, Identity: id, Ready: supervisor.Snapshot{Binding: b, BackendRunning: true, SerialHealthy: true, PinPresent: true, CertificateCurrent: true, ProbeOK: true, ZoneMatches: true}, Network: n, Connection: sshx.Connection{Address: "192.168.64.2", Port: 22, Binding: sb, RuntimeDirectory: r, IdentityFile: r + "/client", CertificateFile: r + "/client-cert.pub", KnownHostsFile: r + "/known_hosts"}, Certificate: sshx.N1CertificateObservation{SHA256: contract.SHA([]byte("cert")), CAFingerprint: "SHA256:" + strings.Repeat("a", 43), Principal: sb.Principal(), Identity: sb.CertificateIdentity(), NotBefore: now, NotAfter: now + 1200}, PrivateKey: worker.FileMetadata{Path: r + "/client", UID: 501, Mode: 0100600, Links: 1, Size: 400}, HostPinSHA: contract.SHA([]byte("pin")), CASHA: contract.SHA([]byte("ca"))}
}
func TestRuntimeRejectsForeignStalePathsExpiredCertAndNonready(t *testing.T) {
	o := runtimeFixture()
	if admitRuntime(o, 0, o.Identity) != nil {
		t.Fatal("fixture invalid")
	}
	mutations := []func(*worker.RuntimeObservation){func(v *worker.RuntimeObservation) { v.Identity.Generation = "40000000-0000-4000-8000-000000000001" }, func(v *worker.RuntimeObservation) { v.Ready.ProbeOK = false }, func(v *worker.RuntimeObservation) { v.Connection.IdentityFile = "/private/tmp/foreign" }, func(v *worker.RuntimeObservation) { v.Certificate.NotBefore = 1; v.Certificate.NotAfter = 1201 }, func(v *worker.RuntimeObservation) { v.Certificate.Principal = "foreign" }, func(v *worker.RuntimeObservation) { v.Network.ConfigSHA256 = contract.CandidateConfigSHA }, func(v *worker.RuntimeObservation) { v.PrivateKey.Links = 2 }, func(v *worker.RuntimeObservation) { v.HostPinSHA = strings.Repeat("z", 64) }}
	for _, mutate := range mutations {
		v := o
		mutate(&v)
		if admitRuntime(v, 0, o.Identity) == nil {
			t.Fatal("foreign/stale runtime admitted")
		}
	}
}
func TestCopyACKIsExactAndUnknownNeverGatesRead(t *testing.T) {
	o := clipboarddiag.Operation{Version: 1, OperationID: "10000000-0000-4000-8000-000000000001", Direction: "write", Domain: config.N1Domain, SessionID: "20000000-0000-4000-8000-000000000001", BackendKind: "tart", BackendObject: "fixed", Generation: "30000000-0000-4000-8000-000000000001", ExpiresAt: time.Unix(100, 0).UTC()}
	r := clipboarddiag.InvocationReceipt{Version: 1, Binding: o, Outcome: "committed", Fragments: []clipboarddiag.Fragment{}, Synthetic: clipboarddiag.SyntheticResult{FixtureID: "n1_clipboard_control_v1", ExpectedSHA256: "9096af926f38bc69facaa4d383ba789f106a6506fadf6cedd170616b882f88e7", Length: len("boxwarden-n1-clipboard-control\n"), Equal: true}}
	if !copyAcknowledged(r, o) {
		t.Fatal("actual ACK refused")
	}
	r.Outcome = "unknown"
	if copyAcknowledged(r, o) {
		t.Fatal("unknown ACK admitted")
	}
	r.Outcome = "committed"
	r.Binding.Generation = "40000000-0000-4000-8000-000000000001"
	if copyAcknowledged(r, o) {
		t.Fatal("foreign ACK admitted")
	}
}
func TestOriginalBudgetsCannotResetAfterAttendanceOrExpiry(t *testing.T) {
	w := testWindow()
	b, _ := newBudget(w, clock.Reading{Wall: 101, Continuous: 110})
	if b.check(clock.Reading{Wall: 201, Continuous: 210}, true) != nil || b.active != 100 || b.attendance != 200 {
		t.Fatal("attendance not charged")
	}
	r := clock.Reading{Wall: w.ExpiresUnixNS, Continuous: w.ContinuousLimitNS}
	if b.check(r, false) == nil || b.check(clock.Reading{Wall: 301, Continuous: 310}, false) == nil {
		t.Fatal("expired original reset")
	}
}
func TestLengthDelimitedArgvWitnessSeparatesBoundaries(t *testing.T) {
	a := argvWitness("/fixed", []string{"one two", ""})
	b := argvWitness("/fixed", []string{"one", "two"})
	if a.LengthDelimitedSHA == b.LengthDelimitedSHA || a.Argc != 3 || a.Arguments[1].Bytes != 7 || a.Arguments[2].Bytes != 0 {
		t.Fatal("argv boundary erased")
	}
}
func TestActualTypedWorkerSchemasRoundTrip(t *testing.T) {
	runtime := runtimeFixture()
	raw, _ := json.Marshal(runtime)
	var got worker.RuntimeObservation
	if decode(append(raw, '\n'), &got, 16384) != nil {
		t.Fatal("actual runtime arrays/schema refused")
	}
	summary := sshx.N1PeerSummary{N1PeerReady: sshx.N1PeerReady{Record: "summary", Version: 1}, Counters: map[string]int{"tcp22_syn_out": 1}, IncompleteReasons: []string{}}
	raw, _ = json.Marshal(summary)
	var received sshx.N1PeerSummary
	if decode(append(raw, '\n'), &received, 8192) != nil || received.Counters["tcp22_syn_out"] != 1 {
		t.Fatal("actual flattened observer/map schema refused")
	}
}
