package sshx

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCAStoreRejectsDuplicateUnknownTrailingAndOversizedMetadata(t *testing.T) {
	root := privateRoot(t)
	work := testDomain(t, "work", root)
	store := NewCAStore(CAStoreOptions{Runner: newKeygenRunner(t), Identity: StaticIdentity{UID: 501, Name: "wes"}, NewUUID: func() (string, error) { return testUUID, nil }})
	if _, err := store.Init(context.Background(), work, []Domain{work}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "identity", "ssh-user-ca", "metadata.json")
	for _, malformed := range []string{
		`{"version":1,"version":1}`,
		`{"unknown":true}`,
		`{} {}`,
		strings.Repeat("x", maxStateFileBytes+1),
	} {
		mustWrite(t, path, []byte(malformed), 0o600)
		if _, err := store.Load(context.Background(), work); err == nil {
			t.Fatalf("Load(%q) error = nil", malformed[:min(len(malformed), 16)])
		}
	}
}

func TestCAFreshStoreLoadsAndIssuesWithoutInit(t *testing.T) {
	root := privateRoot(t)
	work := testDomain(t, "work", root)
	runner := newKeygenRunner(t)
	initial := NewCAStore(CAStoreOptions{Runner: runner, Identity: StaticIdentity{UID: 501, Name: "wes"}, NewUUID: func() (string, error) { return testUUID, nil }})
	if _, err := initial.Init(context.Background(), work, []Domain{work}); err != nil {
		t.Fatal(err)
	}
	runner.commands = nil
	fresh := NewCAStore(CAStoreOptions{Runner: runner, Identity: StaticIdentity{UID: 501, Name: "wes"}})
	ca, err := fresh.Load(context.Background(), work)
	if err != nil {
		t.Fatalf("fresh Load() error = %v", err)
	}
	issuer := NewCertificateIssuer(ca, runner, StaticIdentity{UID: 501, Name: "wes"}, func() time.Time { return time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC) })
	key := filepath.Join(root, "runtime", "client")
	if err := os.Mkdir(filepath.Dir(key), 0o700); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, key, []byte("client-key"), 0o600)
	if _, err := issuer.Issue(context.Background(), testBinding(t, work), filepath.Dir(key), key); err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	for _, command := range runner.commands {
		if len(command.Args) > 0 && command.Args[0] == "-q" {
			t.Fatalf("fresh issue invoked CA creation: %#v", command)
		}
	}
}

func TestCALoadRejectsChangedCreatingOperator(t *testing.T) {
	root := privateRoot(t)
	work := testDomain(t, "work", root)
	runner := newKeygenRunner(t)
	store := NewCAStore(CAStoreOptions{Runner: runner, Identity: StaticIdentity{UID: 501, Name: "wes"}, NewUUID: func() (string, error) { return testUUID, nil }})
	if _, err := store.Init(context.Background(), work, []Domain{work}); err != nil {
		t.Fatal(err)
	}
	changed := NewCAStore(CAStoreOptions{Runner: runner, Identity: StaticIdentity{UID: 501, Name: "other"}})
	if _, err := changed.Load(context.Background(), work); err == nil {
		t.Fatal("Load(changed operator) error = nil")
	}
}

func TestPinStoreRejectsBindingForDifferentDomainRoot(t *testing.T) {
	workRoot, personalRoot := privateRoot(t), privateRoot(t)
	work, personal := testDomain(t, "work", workRoot), testDomain(t, "personal", personalRoot)
	if _, err := NewPinStore(personal).Admit(context.Background(), testBinding(t, work), ObservedHostKey{Algorithm: "ssh-ed25519", PublicKey: testPublicKey}); err == nil {
		t.Fatal("Admit(work binding under personal root) error = nil")
	}
}

func TestCAAndPinStoresRejectUnexpectedStateFiles(t *testing.T) {
	root := privateRoot(t)
	work := testDomain(t, "work", root)
	runner := newKeygenRunner(t)
	caStore := NewCAStore(CAStoreOptions{Runner: runner, Identity: StaticIdentity{UID: 501, Name: "wes"}, NewUUID: func() (string, error) { return testUUID, nil }})
	if _, err := caStore.Init(context.Background(), work, []Domain{work}); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(root, "identity", "ssh-user-ca", "unexpected"), []byte("x"), 0o600)
	if _, err := caStore.Load(context.Background(), work); err == nil {
		t.Fatal("Load(unexpected CA state) error = nil")
	}
	if err := os.Remove(filepath.Join(root, "identity", "ssh-user-ca", "unexpected")); err != nil {
		t.Fatal(err)
	}
	binding := testBinding(t, work)
	pins := NewPinStore(work)
	if _, err := pins.Admit(context.Background(), binding, ObservedHostKey{Algorithm: "ssh-ed25519", PublicKey: testPublicKey}); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(root, "identity", "ssh-host-pins", "unexpected"), []byte("x"), 0o600)
	if _, err := pins.Load(context.Background(), binding); err == nil {
		t.Fatal("Load(unexpected pin state) error = nil")
	}
}

func TestClientRejectsMissingOrUnsafeCredentialFiles(t *testing.T) {
	connection := testConnection(t)
	if err := os.Remove(connection.IdentityFile); err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{onRun: func(Command) Result { return Result{Stdout: `{"version":1,"ok":true}`} }}
	if _, err := NewClient(runner).Probe(context.Background(), connection, ProbeRequest{}); err == nil {
		t.Fatal("Probe(missing runtime credentials) error = nil")
	}
	for _, path := range []string{connection.IdentityFile, connection.CertificateFile, connection.KnownHostsFile} {
		mustWrite(t, path, []byte("x"), 0o600)
	}
	if _, err := NewClient(runner).Probe(context.Background(), connection, ProbeRequest{}); err == nil {
		t.Fatal("Probe(unsafe certificate mode) error = nil")
	}
}

func TestManagementClientPropagatesCancellationAndRejectsTruncatedOutput(t *testing.T) {
	connection := testConnection(t)
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := NewClient(contextRunner{}).Probe(canceled, connection, ProbeRequest{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("Probe(canceled) error = %v", err)
	}
	if _, err := NewClient(&fakeRunner{onRun: func(Command) Result { return Result{Stdout: `{"version":1,"ok":true}`, Truncated: true} }}).Probe(context.Background(), connection, ProbeRequest{}); err == nil {
		t.Fatal("Probe(truncated output) error = nil")
	}
}

func TestCertificateIssuerRejectsCrossDomainBindingWithoutSigning(t *testing.T) {
	root := privateRoot(t)
	work := testDomain(t, "work", root)
	runner := newKeygenRunner(t)
	store := NewCAStore(CAStoreOptions{Runner: runner, Identity: StaticIdentity{UID: 501, Name: "wes"}, NewUUID: func() (string, error) { return testUUID, nil }})
	ca, err := store.Init(context.Background(), work, []Domain{work})
	if err != nil {
		t.Fatal(err)
	}
	key := filepath.Join(root, "runtime", "client")
	if err := os.Mkdir(filepath.Dir(key), 0o700); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, key, []byte("client-key"), 0o600)
	runner.commands = nil
	personal := testDomain(t, "personal", privateRoot(t))
	if _, err := NewCertificateIssuer(ca, runner, StaticIdentity{UID: 501, Name: "wes"}, time.Now).Issue(context.Background(), testBinding(t, personal), filepath.Dir(key), key); err == nil {
		t.Fatal("Issue(cross-domain) error = nil")
	}
	if len(runner.commands) != 0 {
		t.Fatalf("cross-domain Issue invoked runner: %#v", runner.commands)
	}
}

func TestReadZoneRejectsAmbiguousResponses(t *testing.T) {
	for _, response := range []string{`{"version":1,"zone":"UTC","zone":"UTC"}`, `{"version":1,"zone":"UTC","x":1}`, `{"version":1,"zone":"UTC"} {}`} {
		runner := &fakeRunner{onRun: func(Command) Result { return Result{Stdout: response} }}
		if _, err := NewClient(runner).ReadZone(context.Background(), testConnection(t), ReadZoneRequest{}); err == nil {
			t.Fatalf("ReadZone(%q) error = nil", response)
		}
	}
}

func TestManagementWallDeadlineCapsLongCallerDeadline(t *testing.T) {
	connection := testConnection(t)
	for _, test := range []struct {
		name       string
		context    func() (context.Context, context.CancelFunc)
		wantAtMost time.Duration
	}{
		{"none", func() (context.Context, context.CancelFunc) { return context.WithCancel(context.Background()) }, managementWallTimeout},
		{"shorter", func() (context.Context, context.CancelFunc) {
			return context.WithTimeout(context.Background(), time.Second)
		}, time.Second},
		{"longer", func() (context.Context, context.CancelFunc) {
			return context.WithTimeout(context.Background(), 10*time.Minute)
		}, managementWallTimeout},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := test.context()
			defer cancel()
			runner := &deadlineRunner{}
			if _, err := NewClient(runner).Probe(ctx, connection, ProbeRequest{}); err != nil {
				t.Fatal(err)
			}
			if runner.deadline.Sub(time.Now()) > test.wantAtMost+time.Second {
				t.Fatalf("deadline exceeds cap: %v", runner.deadline.Sub(time.Now()))
			}
		})
	}
}

type deadlineRunner struct{ deadline time.Time }

func (r *deadlineRunner) Run(ctx context.Context, _ Command) (Result, error) {
	r.deadline, _ = ctx.Deadline()
	return Result{Stdout: `{"version":1,"ok":true}`}, nil
}

func TestCertificateInspectionRejectsAmbiguousSecurityFields(t *testing.T) {
	certificate := Certificate{Identity: "boxwarden:work:" + testUUID, Principal: "boxwarden-session-" + testUUID, NotBefore: time.Date(2026, 9, 1, 11, 55, 0, 0, time.UTC), NotAfter: time.Date(2026, 9, 1, 12, 15, 0, 0, time.UTC)}
	valid := certificateInspection(certificate, certificate.Principal, "(none)", "(none)", "2026-09-01T11:55:00 to 2026-09-01T12:15:00")
	if !validCertificateInspection(valid, certificate) {
		t.Fatal("validCertificateInspection(valid) = false")
	}
	for _, output := range []string{
		certificateInspection(certificate, certificate.Principal+"\n        another", "(none)", "(none)", "2026-09-01T11:55:00 to 2026-09-01T12:15:00"),
		certificateInspection(certificate, certificate.Principal, "force-command=/bin/sh", "(none)", "2026-09-01T11:55:00 to 2026-09-01T12:15:00"),
		certificateInspection(certificate, certificate.Principal, "(none)", "permit-pty", "2026-09-01T11:55:00 to 2026-09-01T12:15:00"),
		certificateInspection(certificate, certificate.Principal, "(none)", "(none)", "2026-09-01T11:55:03 to 2026-09-01T12:15:03"),
		"noise ssh-ed25519-cert-v01@openssh.com Key ID: \"" + certificate.Identity + "\" " + certificate.Principal + " Extensions: (none)",
	} {
		if validCertificateInspection(output, certificate) {
			t.Fatalf("validCertificateInspection accepted %q", output)
		}
	}
}

func certificateInspection(c Certificate, principals, critical, extensions, validity string) string {
	return "Type: ssh-ed25519-cert-v01@openssh.com user certificate\nKey ID: \"" + c.Identity + "\"\nValid: from " + validity + "\nPrincipals:\n        " + principals + "\nCritical Options: " + critical + "\nExtensions: " + extensions + "\n"
}

type contextRunner struct{}

func (contextRunner) Run(ctx context.Context, _ Command) (Result, error) { return Result{}, ctx.Err() }

func TestPinStoreRejectsDuplicateUnknownTrailingAndOversizedJSON(t *testing.T) {
	root := privateRoot(t)
	work := testDomain(t, "work", root)
	store := NewPinStore(work)
	binding := testBinding(t, work)
	if _, err := store.Admit(context.Background(), binding, ObservedHostKey{Algorithm: "ssh-ed25519", PublicKey: testPublicKey}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "identity", "ssh-host-pins", testUUID+".json")
	for _, malformed := range []string{`{"version":1,"version":1}`, `{"unknown":true}`, `{} {}`, strings.Repeat("x", maxStateFileBytes+1)} {
		mustWrite(t, path, []byte(malformed), 0o600)
		if _, err := store.Load(context.Background(), binding); err == nil {
			t.Fatalf("Load(%q) error = nil", malformed[:min(len(malformed), 16)])
		}
	}
}

func TestManagementResponsesRejectDuplicateUnknownTrailingAndOversizedJSON(t *testing.T) {
	for _, response := range []string{`{"version":1,"ok":true,"ok":true}`, `{"version":1,"ok":true,"unknown":1}`, `{"version":1,"ok":true} {}`, strings.Repeat("x", maxManagementResponseBytes+1)} {
		runner := &fakeRunner{onRun: func(Command) Result { return Result{Stdout: response} }}
		if _, err := NewClient(runner).Probe(context.Background(), testConnection(t), ProbeRequest{}); err == nil {
			t.Fatalf("Probe(%q) error = nil", response[:min(len(response), 16)])
		}
	}
}

func TestCAInitNormalizesSSHKeygenPublicMode(t *testing.T) {
	root := privateRoot(t)
	work := testDomain(t, "work", root)
	runner := newKeygenRunner(t)
	store := NewCAStore(CAStoreOptions{Runner: runner, Identity: StaticIdentity{UID: 501, Name: "wes"}, NewUUID: func() (string, error) { return testUUID, nil }})
	if _, err := store.Init(context.Background(), work, []Domain{work}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(filepath.Join(root, "identity", "ssh-user-ca", "ca.pub"))
	if err != nil || info.Mode().Perm() != 0o644 {
		t.Fatalf("ca.pub = %v, %v; want 0644", info, err)
	}
}
