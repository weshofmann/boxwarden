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

func TestCertificateIssuerUsesExactNoExtensionCertificateArguments(t *testing.T) {
	root := privateRoot(t)
	work := testDomain(t, "work", root)
	runner := newKeygenRunner(t)
	store := NewCAStore(CAStoreOptions{Runner: runner, Identity: StaticIdentity{UID: 501, Name: "wes"}, NewUUID: func() (string, error) { return testUUID, nil }})
	ca, err := store.Init(context.Background(), work, []Domain{work})
	if err != nil {
		t.Fatal(err)
	}
	runner.commands = nil
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	issuer := NewCertificateIssuer(ca, runner, StaticIdentity{UID: 501, Name: "wes"}, func() time.Time { return now })
	binding := testBinding(t, work)
	key := filepath.Join(root, "runtime", "client")
	if err := os.Mkdir(filepath.Dir(key), 0o700); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, key, []byte("client-key"), 0o600)
	certificate, err := issuer.Issue(context.Background(), binding, filepath.Dir(key), key)
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	wantPrincipal := "boxwarden-session-" + testUUID
	wantIdentity := "boxwarden:work:" + testUUID
	if certificate.Principal != wantPrincipal || certificate.Identity != wantIdentity || !certificate.NotAfter.Equal(now.Add(15*time.Minute)) {
		t.Fatalf("certificate = %#v", certificate)
	}
	if len(runner.commands) != 3 {
		t.Fatalf("issue commands = %#v", runner.commands)
	}
	temporaryKey := runner.commands[1].Args[len(runner.commands[1].Args)-1]
	if temporaryKey == key || filepath.Dir(temporaryKey) != filepath.Dir(key) || filepath.Base(temporaryKey) == filepath.Base(key) {
		t.Fatalf("temporary signing key = %q, want unpredictable sibling of %q", temporaryKey, key)
	}
	want := []string{"-s", filepath.Join(root, "identity", "ssh-user-ca", "ca"), "-I", wantIdentity, "-n", wantPrincipal, "-V", "-5m:+15m", "-O", "clear", temporaryKey}
	if !sameStrings(runner.commands[1].Args, want) || !sameStrings(runner.commands[2].Args, []string{"-L", "-f", temporaryKey + "-cert.pub"}) {
		t.Fatalf("issue argv = %#v", runner.commands)
	}
	if _, err := os.Lstat(temporaryKey); !os.IsNotExist(err) {
		t.Fatalf("temporary client key remains after issue: %v", err)
	}
	if _, err := os.Lstat(temporaryKey + "-cert.pub"); !os.IsNotExist(err) {
		t.Fatalf("temporary certificate remains after issue: %v", err)
	}
	if contents, err := os.ReadFile(key); err != nil || string(contents) != "client-key" {
		t.Fatalf("client key changed = %q, %v", contents, err)
	}
}

func TestCertificateRenewalRequiredAtFiveMinuteThreshold(t *testing.T) {
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	certificate := Certificate{NotAfter: now.Add(6 * time.Minute)}
	if RenewalRequired(certificate, now) {
		t.Fatal("RenewalRequired(6m remaining) = true")
	}
	if !RenewalRequired(certificate, now.Add(time.Minute)) {
		t.Fatal("RenewalRequired(5m remaining) = false")
	}
	if !RenewalRequired(Certificate{NotAfter: now}, now) {
		t.Fatal("RenewalRequired(expired) = false")
	}
}

func TestCertificateInspectionAcceptsUTCValidityForNonUTCIssuerClock(t *testing.T) {
	location := time.FixedZone("operator-local", -7*60*60)
	now := time.Date(2026, 9, 1, 5, 0, 0, 0, location)
	certificate := Certificate{
		Identity:  "boxwarden:work:" + testUUID,
		Principal: "boxwarden-session-" + testUUID,
		NotBefore: now.Add(-renewalWindow).UTC(),
		NotAfter:  now.Add(certificateLifetime).UTC(),
	}
	inspection := certificateInspection(certificate, certificate.Principal, "(none)", "(none)", "2026-09-01T11:55:00 to 2026-09-01T12:15:00")
	if !validCertificateInspection(inspection, certificate) {
		t.Fatal("validCertificateInspection() rejected UTC ssh-keygen output for non-UTC issuer clock")
	}
}

func TestCertificateInspectionAllowsOnlySchedulingToleranceAroundFixedValidity(t *testing.T) {
	certificate := Certificate{
		Identity:  "boxwarden:work:" + testUUID,
		Principal: "boxwarden-session-" + testUUID,
		NotBefore: time.Date(2026, 9, 1, 11, 55, 0, 0, time.UTC),
		NotAfter:  time.Date(2026, 9, 1, 12, 15, 0, 0, time.UTC),
	}
	withinTolerance := certificateInspection(certificate, certificate.Principal, "(none)", "(none)", "2026-09-01T11:55:01 to 2026-09-01T12:15:01")
	if !validCertificateInspection(withinTolerance, certificate) {
		t.Fatal("validCertificateInspection() rejected one-second ssh-keygen scheduling delay")
	}
	outsideTolerance := certificateInspection(certificate, certificate.Principal, "(none)", "(none)", "2026-09-01T11:55:03 to 2026-09-01T12:15:03")
	if validCertificateInspection(outsideTolerance, certificate) {
		t.Fatal("validCertificateInspection() accepted validity outside scheduling tolerance")
	}
}

func TestCertificateIssueCapsWallDeadline(t *testing.T) {
	root := privateRoot(t)
	work := testDomain(t, "work", root)
	setup := NewCAStore(CAStoreOptions{Runner: newKeygenRunner(t), Identity: StaticIdentity{UID: 501, Name: "wes"}, NewUUID: func() (string, error) { return testUUID, nil }})
	ca, err := setup.Init(context.Background(), work, []Domain{work})
	if err != nil {
		t.Fatal(err)
	}
	runner := &deadlineCapturingRunner{Runner: newKeygenRunner(t)}
	issuer := NewCertificateIssuer(ca, runner, StaticIdentity{UID: 501, Name: "wes"}, func() time.Time {
		return time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	})
	key := filepath.Join(root, "runtime", "client")
	if err := os.Mkdir(filepath.Dir(key), 0o700); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, key, []byte("client-key"), 0o600)
	if _, err := issuer.Issue(context.Background(), testBinding(t, work), filepath.Dir(key), key); err != nil {
		t.Fatal(err)
	}
	assertDeadlineWithin(t, runner.deadlines, caWallTimeout)
}

func TestCertificateIssuerRefusesUnsafeExistingOutputBeforeSigning(t *testing.T) {
	for _, test := range []struct {
		name  string
		setup func(t *testing.T, output string)
	}{
		{"symlink", func(t *testing.T, output string) {
			victim := filepath.Join(filepath.Dir(output), "victim")
			mustWrite(t, victim, []byte("victim"), 0o600)
			if err := os.Symlink(victim, output); err != nil {
				t.Fatal(err)
			}
		}},
		{"hardlink", func(t *testing.T, output string) {
			victim := filepath.Join(filepath.Dir(output), "victim")
			mustWrite(t, victim, []byte("victim"), 0o644)
			if err := os.Link(victim, output); err != nil {
				t.Fatal(err)
			}
		}},
		{"unsafe mode", func(t *testing.T, output string) {
			mustWrite(t, output, []byte("unsafe certificate"), 0o600)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := privateRoot(t)
			work := testDomain(t, "work", root)
			runner := newKeygenRunner(t)
			store := NewCAStore(CAStoreOptions{Runner: runner, Identity: StaticIdentity{UID: 501, Name: "wes"}, NewUUID: func() (string, error) { return testUUID, nil }})
			ca, err := store.Init(context.Background(), work, []Domain{work})
			if err != nil {
				t.Fatal(err)
			}
			runtime := filepath.Join(root, "runtime")
			if err := os.Mkdir(runtime, 0o700); err != nil {
				t.Fatal(err)
			}
			key := filepath.Join(runtime, "client")
			mustWrite(t, key, []byte("client-key"), 0o600)
			output := key + "-cert.pub"
			test.setup(t, output)
			runner.commands = nil
			if _, err := NewCertificateIssuer(ca, runner, StaticIdentity{UID: 501, Name: "wes"}, func() time.Time { return time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC) }).Issue(context.Background(), testBinding(t, work), runtime, key); err == nil {
				t.Fatal("Issue() accepted unsafe existing certificate output")
			}
			for _, command := range runner.commands {
				if len(command.Args) > 0 && command.Args[0] == "-s" {
					t.Fatalf("Issue() signed despite unsafe output: %#v", command)
				}
			}
			if contents, err := os.ReadFile(filepath.Join(runtime, "victim")); err == nil && string(contents) != "victim" {
				t.Fatalf("unsafe output modified victim = %q", contents)
			}
		})
	}
}

func TestCertificateIssuerSafelyRenewsExistingCertificate(t *testing.T) {
	root := privateRoot(t)
	work := testDomain(t, "work", root)
	runner := newKeygenRunner(t)
	store := NewCAStore(CAStoreOptions{Runner: runner, Identity: StaticIdentity{UID: 501, Name: "wes"}, NewUUID: func() (string, error) { return testUUID, nil }})
	ca, err := store.Init(context.Background(), work, []Domain{work})
	if err != nil {
		t.Fatal(err)
	}
	runtime := filepath.Join(root, "runtime")
	if err := os.Mkdir(runtime, 0o700); err != nil {
		t.Fatal(err)
	}
	key := filepath.Join(runtime, "client")
	mustWrite(t, key, []byte("client-key"), 0o600)
	mustWrite(t, key+"-cert.pub", []byte("old certificate"), 0o644)
	if _, err := NewCertificateIssuer(ca, runner, StaticIdentity{UID: 501, Name: "wes"}, func() time.Time { return time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC) }).Issue(context.Background(), testBinding(t, work), runtime, key); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(key + "-cert.pub")
	if err != nil || string(contents) != "certificate\n" {
		t.Fatalf("renewed certificate = %q, %v", contents, err)
	}
}

func TestCertificateIssuerInterruptionCleansOnlyTemporaryState(t *testing.T) {
	root := privateRoot(t)
	work := testDomain(t, "work", root)
	setup := NewCAStore(CAStoreOptions{Runner: newKeygenRunner(t), Identity: StaticIdentity{UID: 501, Name: "wes"}, NewUUID: func() (string, error) { return testUUID, nil }})
	ca, err := setup.Init(context.Background(), work, []Domain{work})
	if err != nil {
		t.Fatal(err)
	}
	runtime := filepath.Join(root, "runtime")
	if err := os.Mkdir(runtime, 0o700); err != nil {
		t.Fatal(err)
	}
	key := filepath.Join(runtime, "client")
	mustWrite(t, key, []byte("client-key"), 0o600)
	interrupted := errors.New("sign interrupted")
	runner := newKeygenRunner(t)
	original := runner.onRun
	runner.onRun = func(command Command) Result {
		if len(command.Args) > 0 && command.Args[0] == "-s" {
			runner.err = interrupted
		}
		return original(command)
	}
	issuer := NewCertificateIssuer(ca, runner, StaticIdentity{UID: 501, Name: "wes"}, func() time.Time { return time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC) })
	_, err = issuer.Issue(context.Background(), testBinding(t, work), runtime, key)
	if !errors.Is(err, interrupted) || strings.Contains(err.Error(), "client-key") {
		t.Fatalf("Issue() interruption error = %v", err)
	}
	if _, err := os.Lstat(key + "-cert.pub"); !os.IsNotExist(err) {
		t.Fatalf("interrupted issue created final certificate: %v", err)
	}
	entries, err := os.ReadDir(runtime)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "client" {
		t.Fatalf("interrupted issue left runtime entries %#v", entries)
	}
	contents, err := os.ReadFile(key)
	if err != nil || string(contents) != "client-key" {
		t.Fatalf("client key changed = %q, %v", contents, err)
	}
}

func TestCertificateIssuerRejectsLateCertificateCollisionWithoutReplacement(t *testing.T) {
	root := privateRoot(t)
	work := testDomain(t, "work", root)
	runner := newKeygenRunner(t)
	store := NewCAStore(CAStoreOptions{Runner: runner, Identity: StaticIdentity{UID: 501, Name: "wes"}, NewUUID: func() (string, error) { return testUUID, nil }})
	ca, err := store.Init(context.Background(), work, []Domain{work})
	if err != nil {
		t.Fatal(err)
	}
	runtime := filepath.Join(root, "runtime")
	if err := os.Mkdir(runtime, 0o700); err != nil {
		t.Fatal(err)
	}
	key := filepath.Join(runtime, "client")
	mustWrite(t, key, []byte("client-key"), 0o600)
	original := runner.onRun
	runner.onRun = func(command Command) Result {
		if len(command.Args) > 0 && command.Args[0] == "-L" {
			mustWrite(t, key+"-cert.pub", []byte("late collision"), 0o644)
		}
		return original(command)
	}
	if _, err := NewCertificateIssuer(ca, runner, StaticIdentity{UID: 501, Name: "wes"}, func() time.Time { return time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC) }).Issue(context.Background(), testBinding(t, work), runtime, key); err == nil {
		t.Fatal("Issue() accepted a final-path collision during publication")
	}
	contents, err := os.ReadFile(key + "-cert.pub")
	if err != nil || string(contents) != "late collision" {
		t.Fatalf("late collision changed = %q, %v", contents, err)
	}
}
