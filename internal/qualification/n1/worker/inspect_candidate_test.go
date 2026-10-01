//go:build darwin && cgo && n1diagnostic && n1clipboarddiagnostic && !n1candidate

package worker

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/weshofmann/boxwarden/internal/backend"
	"github.com/weshofmann/boxwarden/internal/backend/fake"
	"github.com/weshofmann/boxwarden/internal/config"
	"github.com/weshofmann/boxwarden/internal/networkdiag"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/clock"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/contract"
	"github.com/weshofmann/boxwarden/internal/session"
	"github.com/weshofmann/boxwarden/internal/sshx"
	"github.com/weshofmann/boxwarden/internal/supervisor"
)

type inspectReader struct {
	n            networkdiag.Inspection
	launch       networkdiag.LaunchObservation
	snapshots    int
	launches     int
	onFinal      func()
	changedBirth bool
}

func (r *inspectReader) Snapshot(_ context.Context, b supervisor.Binding) (supervisor.Snapshot, error) {
	r.snapshots++
	if r.snapshots == 2 && r.onFinal != nil {
		r.onFinal()
	}
	return supervisor.Snapshot{Binding: b, BackendRunning: true, SerialHealthy: true, PinPresent: true, CertificateCurrent: true, ProbeOK: true, ZoneMatches: true, ObservedAt: time.Now(), Diagnostic: "private error must never escape"}, nil
}
func (r *inspectReader) InspectDiagnosticNetwork(context.Context, supervisor.Binding) (networkdiag.Inspection, error) {
	r.n.ObservedUnixNS = uint64(time.Now().UnixNano())
	return r.n, nil
}
func (r *inspectReader) ObserveDiagnosticLaunch(context.Context, supervisor.Binding) (networkdiag.LaunchObservation, error) {
	r.launches++
	v := r.launch
	v.Inspection = r.n
	v.Inspection.ObservedUnixNS = uint64(time.Now().UnixNano())
	n, e := clock.Now()
	if e != nil {
		return v, e
	}
	v.Watch.Observed = networkdiag.ClockReading{WallNS: n.Wall, ContinuousNS: n.Continuous}
	if r.changedBirth && r.launches > 1 {
		v.Child.UniqueID++
	}
	return v, nil
}
func wireString(b []byte) []byte {
	out := make([]byte, 4)
	binary.BigEndian.PutUint32(out, uint32(len(b)))
	return append(out, b...)
}
func wireU64(n uint64) []byte { out := make([]byte, 8); binary.BigEndian.PutUint64(out, n); return out }
func signedCert(ca ed25519.PrivateKey, client []byte, b sshx.Binding, before uint64) []byte {
	key := append(wireString([]byte("ssh-ed25519")), wireString(ca.Public().(ed25519.PublicKey))...)
	v := append(wireString([]byte("ssh-ed25519-cert-v01@openssh.com")), wireString([]byte("nonce"))...)
	v = append(v, wireString(client)...)
	v = append(v, wireU64(before)...)
	v = append(v, 0, 0, 0, 1)
	v = append(v, wireString([]byte(b.CertificateIdentity()))...)
	v = append(v, wireString(wireString([]byte(b.Principal())))...)
	v = append(v, wireU64(before)...)
	v = append(v, wireU64(before+1200)...)
	for i := 0; i < 3; i++ {
		v = append(v, wireString(nil)...)
	}
	v = append(v, wireString(key)...)
	sig := append(wireString([]byte("ssh-ed25519")), wireString(ed25519.Sign(ca, v))...)
	v = append(v, wireString(sig)...)
	return []byte("ssh-ed25519-cert-v01@openssh.com " + base64.StdEncoding.EncodeToString(v) + "\n")
}
func inspectFixture(t *testing.T) (*Worker, Identity, *inspectReader, func()) {
	t.Helper()
	root, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	backendFake := fake.New(backend.Observation{ObjectID: contract.BaseName, Exists: true, State: backend.ObjectStopped})
	w := &Worker{domain: config.Domain{ID: config.N1Domain, StateRoot: root}, observer: backendFake, creator: backendFake}
	created, e := w.Create(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	rec := created.Record
	rec.StartGeneration = "22222222-2222-4222-8222-222222222222"
	rec.IntendedState = session.StateRunning
	rec.Readiness.Status = session.ReadinessReady
	if e = session.SaveRecord(root, config.N1Domain, rec); e != nil {
		t.Fatal(e)
	}
	id := Identity{rec.ID, rec.Backend.ObjectID, rec.StartGeneration}
	b := sshx.Binding{Domain: config.N1Domain, SessionID: id.Session, BackendKind: "tart", BackendObject: id.Backend}
	host, _, e := ed25519.GenerateKey(rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	hostWire := append(wireString([]byte("ssh-ed25519")), wireString(host)...)
	hostPublic := "ssh-ed25519 " + base64.StdEncoding.EncodeToString(hostWire)
	pin, e := sshx.NewPinStore(sshx.Domain{ID: config.N1Domain, StateRoot: root}).Admit(context.Background(), b, sshx.ObservedHostKey{Algorithm: "ssh-ed25519", PublicKey: hostPublic})
	if e != nil {
		t.Fatal(e)
	}
	runtime := root + "/runtime/n1qualification/" + id.Session + "/" + id.Generation
	if e = os.MkdirAll(runtime, 0700); e != nil {
		t.Fatal(e)
	}
	if _, e = sshx.WriteKnownHosts(runtime, pin); e != nil {
		t.Fatal(e)
	}
	ca, privateCA, e := ed25519.GenerateKey(rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	caWire := append(wireString([]byte("ssh-ed25519")), wireString(ca)...)
	digest := sha256.Sum256(caWire)
	caPublic := contract.CAPublic{Version: 1, Domain: config.N1Domain, Algorithm: "ssh-ed25519", PublicKey: "ssh-ed25519 " + base64.StdEncoding.EncodeToString(caWire), PublicDigest: hex.EncodeToString(digest[:]), Fingerprint: "SHA256:" + base64.RawStdEncoding.EncodeToString(digest[:]), CreationUUID: "33333333-3333-4333-8333-333333333333", CreatorUID: 501, CreatorName: "devel"}
	caRaw, e := json.Marshal(caPublic)
	if e != nil {
		t.Fatal(e)
	}
	caDir := root + "/identity/ssh-user-ca"
	if e := os.Mkdir(caDir, 0700); e != nil {
		t.Fatal(e)
	}
	fixtureWrite(t, caDir+"/metadata.json", append(caRaw, '\n'), 0600)
	fixtureWrite(t, caDir+"/ca.pub", []byte(caPublic.PublicKey+"\n"), 0644)

	client, _, e := ed25519.GenerateKey(rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	clientWire := append(wireString([]byte("ssh-ed25519")), wireString(client)...)
	fixtureWrite(t, runtime+"/client", []byte("synthetic never read or hashed"), 0600)
	fixtureWrite(t, runtime+"/client.pub", []byte("ssh-ed25519 "+base64.StdEncoding.EncodeToString(clientWire)+"\n"), 0644)

	before := uint64(time.Now().Unix() - 300)
	writeCert := func() {
		fixtureWrite(t, runtime+"/client-cert.pub", signedCert(privateCA, client, b, before), 0644)

	}
	writeCert()
	now, e := clock.Now()
	if e != nil {
		t.Fatal(e)
	}
	anchor := networkdiag.ClockReading{WallNS: now.Wall - 1e9, ContinuousNS: now.Continuous - 1e9}
	n := networkdiag.Inspection{Binding: networkdiag.Binding{Domain: config.N1Domain, SessionID: id.Session, Generation: id.Generation, BackendKind: "tart", BackendObject: id.Backend, Address: [4]uint8{192, 168, 64, 2}, MAC: [6]uint8{2, 0, 0, 0, 0, 2}}, PinFingerprint: pin.Fingerprint, ConfigSHA256: strings.Repeat("a", 64), ObservedUnixNS: now.Wall}
	r := &inspectReader{n: n, launch: networkdiag.LaunchObservation{Version: 1, Inspection: n, Owner: networkdiag.ProcessCorrelation{PID: 100, BirthUS: 2, UniqueID: 3}, Child: networkdiag.ProcessCorrelation{PID: 101, BirthUS: 4, UniqueID: 5}, Watch: networkdiag.WatchObservation{Hello: networkdiag.Hello{Version: 1, Kind: "HELLO", Generation: id.Generation, Nonce: "44444444-4444-4444-8444-444444444444", CandidateMAC: n.Binding.MAC, Gateway: [4]uint8{192, 168, 64, 1}}, Phase: "available", Anchor: anchor, Deadline: networkdiag.ClockReading{WallNS: anchor.WallNS + uint64(networkdiag.PrearmCap), ContinuousNS: anchor.ContinuousNS + uint64(networkdiag.PrearmCap)}}}}
	w.reader = r
	return w, id, r, func() { before++; writeCert() }
}
func TestWorkerActualPublicRuntimeInspectionAndRenewal(t *testing.T) {
	w, id, r, renew := inspectFixture(t)
	o, e := w.Inspect(context.Background(), id)
	if e != nil {
		t.Fatal(e)
	}
	if o.Ready.Diagnostic != "" || o.PrivateKey.Path == "" || o.Launch == nil {
		t.Fatal("missing public normalized observation")
	}
	renew()
	r.snapshots = 0
	r.launches = 0
	next, e := w.Inspect(context.Background(), id)
	if e != nil || next.Certificate.SHA256 == o.Certificate.SHA256 || next.Launch.Anchor != o.Launch.Anchor || next.Connection != o.Connection {
		t.Fatal("same identity renewal refused or reset", e)
	}
}
func TestWorkerRetainedBirthReplacementRefuses(t *testing.T) {
	w, id, r, _ := inspectFixture(t)
	r.changedBirth = true
	if _, e := w.Inspect(context.Background(), id); e == nil {
		t.Fatal("changed retained process accepted")
	}
}
func TestWorkerCAPublicMutationAtFinalSnapshotRefuses(t *testing.T) {
	w, id, r, _ := inspectFixture(t)
	r.onFinal = func() {
		fixtureWrite(t, w.domain.StateRoot+"/identity/ssh-user-ca/ca.pub", []byte("ssh-ed25519 broken\n"), 0644)
	}
	if _, e := w.Inspect(context.Background(), id); e == nil {
		t.Fatal("public CA changed after captured admission accepted")
	}
}

func TestWorkerOtherPeerMutationAtFinalReadyRefuses(t *testing.T) {
	w, id, r, _ := inspectFixture(t)
	record, e := session.LoadRecord(w.domain.StateRoot, config.N1Domain, roleName)
	if e != nil {
		t.Fatal(e)
	}
	record.ID = "55555555-5555-4555-8555-555555555555"
	record.Name = session.Name(config.N1ControlName)
	if e = session.SaveRecord(w.domain.StateRoot, config.N1Domain, record); e != nil {
		t.Fatal(e)
	}
	r.n.Binding.SessionID = record.ID
	r.n.Binding.BackendObject = id.Backend
	peer := sshx.N1Peer{SessionUUID: record.ID, GenerationUUID: id.Generation, Backend: id.Backend, Address: "192.168.64.2", MAC: "02:00:00:00:00:02"}
	if e := w.admitOther(context.Background(), peer); e != nil {
		t.Fatal("exact peer positive control", e)
	}
	r.snapshots = 0
	r.onFinal = func() { r.n.Binding.Address[3]++ }
	if w.admitOther(context.Background(), peer) == nil {
		t.Fatal("peer address changed at final READY accepted")
	}
}

type leafMutation struct {
	t      *testing.T
	called *bool
}

func (a leafMutation) HasExtendedACL(p string) (bool, error) {
	a.t.Helper()
	*a.called = true
	if e := os.WriteFile(p, []byte("different private metadata length"), 0600); e != nil {
		a.t.Fatal(e)
	}
	return false, nil
}
func TestWorkerLeafMutationDuringACLRefuses(t *testing.T) {
	p := t.TempDir() + "/key"
	if e := os.WriteFile(p, []byte("original"), 0600); e != nil {
		t.Fatal(e)
	}
	canonical, e := filepath.EvalSymlinks(p)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = metadata(canonical, 0600); e != nil {
		t.Fatal("exact private metadata positive control", e)
	}
	called := false
	if _, e = metadataWith(canonical, 0600, leafMutation{t, &called}); e == nil {
		t.Fatal("pathname ACL query changed private leaf after captured stat")
	}
	if !called {
		t.Fatal("mutation control not reached")
	}
}

func fixtureWrite(t *testing.T, p string, raw []byte, mode os.FileMode) {
	t.Helper()
	if e := os.WriteFile(p, raw, mode); e != nil {
		t.Fatal(e)
	}
	if e := os.Chmod(p, mode); e != nil {
		t.Fatal(e)
	}
}

type unrelatedEntry struct {
	t      *testing.T
	called *bool
}

func (a unrelatedEntry) HasExtendedACL(p string) (bool, error) {
	*a.called = true
	if e := os.WriteFile(p+".unrelated", []byte("ordinary sibling"), 0600); e != nil {
		a.t.Fatal(e)
	}
	return false, nil
}
func TestWorkerAncestorEntryChangeKeepsPrivateIdentity(t *testing.T) {
	p := t.TempDir() + "/key"
	fixtureWrite(t, p, []byte("original"), 0600)
	p, e := filepath.EvalSymlinks(p)
	if e != nil {
		t.Fatal(e)
	}
	called := false
	before, e := os.Stat(filepath.Dir(p))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = metadataWith(p, 0600, unrelatedEntry{t, &called}); e != nil {
		after, se := os.Stat(filepath.Dir(p))
		if se != nil {
			t.Fatal(se)
		}
		t.Fatalf("ordinary sibling refused: mutation_reached=%v before=%+v after=%+v error=%v", called, before.Sys(), after.Sys(), e)
	}
	if !called {
		t.Fatal("positive control not reached")
	}
}

type ancestorACLMutation struct {
	t      *testing.T
	called *bool
}

func (a ancestorACLMutation) HasExtendedACL(p string) (bool, error) {
	*a.called = true
	principal, e := workerACLFixturePrincipal(os.Getuid(), user.LookupId)
	if e != nil {
		a.t.Fatal("current private fixture account", e)
	}
	cmd := exec.Command("/bin/chmod", "+a", principal+" allow list", filepath.Dir(p))
	cmd.Env = []string{"LANG=C", "LC_ALL=C"}
	if raw, e := cmd.CombinedOutput(); e != nil {
		a.t.Fatal("private fixture ACL mutation", e, string(raw))
	}
	return false, nil
}

func workerACLFixturePrincipal(uid int, lookup func(string) (*user.User, error)) (string, error) {
	id := strconv.Itoa(uid)
	u, e := lookup(id)
	if e != nil || u == nil || u.Uid != id || u.Username == "" {
		return "", errors.New("fixture account refused")
	}
	return "user:" + u.Username, nil
}

func TestWorkerACLFixtureUsesCurrentAccount(t *testing.T) {
	got, e := workerACLFixturePrincipal(1001, func(id string) (*user.User, error) {
		if id != "1001" {
			t.Fatal("foreign lookup uid", id)
		}
		return &user.User{Uid: id, Username: "runner"}, nil
	})
	if e != nil || got != "user:runner" {
		t.Fatal("fixture selected foreign account", got, e)
	}
	for _, kind := range []string{"error", "nil", "foreign-uid", "empty-name"} {
		t.Run(kind, func(t *testing.T) {
			_, e := workerACLFixturePrincipal(1001, func(id string) (*user.User, error) {
				switch kind {
				case "error":
					return nil, errors.New("lookup")
				case "nil":
					return nil, nil
				case "foreign-uid":
					return &user.User{Uid: "501", Username: "devel"}, nil
				default:
					return &user.User{Uid: id}, nil
				}
			})
			if e == nil {
				t.Fatal("invalid fixture identity admitted")
			}
		})
	}
}
func TestWorkerFinalAncestorACLMutationRefuses(t *testing.T) {
	p := t.TempDir() + "/key"
	fixtureWrite(t, p, []byte("original"), 0600)
	p, e := filepath.EvalSymlinks(p)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = metadata(p, 0600); e != nil {
		t.Fatal("private metadata positive control", e)
	}
	called := false
	if _, e = metadataWith(p, 0600, ancestorACLMutation{t, &called}); e == nil {
		t.Fatal("ancestor ACL changed during leaf inspection accepted")
	}
	if !called {
		t.Fatal("actual ancestor mutation not reached")
	}
}

func TestWorkerDiscoverDerivesFixedCurrentGeneration(t *testing.T) {
	w, id, r, _ := inspectFixture(t)
	fresh := id
	fresh.Generation = ""
	got, e := w.Discover(context.Background(), fresh)
	if e != nil || got.Identity != id || got.Launch == nil {
		t.Fatal("fixed created pair discovery", got, e)
	}
	r.snapshots = 0
	r.launches = 0
	if _, e = w.Discover(context.Background(), id); e == nil || r.snapshots != 0 {
		t.Fatal("caller selected discovery generation")
	}
	fresh.Session = "55555555-5555-4555-8555-555555555555"
	fresh.Backend = createdBackend(fresh.Session)
	if _, e = w.Discover(context.Background(), fresh); e == nil {
		t.Fatal("foreign created pair adopted")
	}
}
