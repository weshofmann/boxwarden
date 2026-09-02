package sshx

import (
	"context"
	"os"
	"path/filepath"
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
	certificate, err := issuer.Issue(context.Background(), binding, key)
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	wantPrincipal := "boxwarden-session-" + testUUID
	wantIdentity := "boxwarden:work:" + testUUID
	if certificate.Principal != wantPrincipal || certificate.Identity != wantIdentity || !certificate.NotAfter.Equal(now.Add(15*time.Minute)) {
		t.Fatalf("certificate = %#v", certificate)
	}
	want := []string{"-s", filepath.Join(root, "identity", "ssh-user-ca", "ca"), "-I", wantIdentity, "-n", wantPrincipal, "-V", "-5m:+15m", "-O", "clear", key}
	if len(runner.commands) != 3 || !sameStrings(runner.commands[1].Args, want) {
		t.Fatalf("issue argv = %#v, want %#v", runner.commands, want)
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
	if _, err := issuer.Issue(context.Background(), testBinding(t, work), key); err != nil {
		t.Fatal(err)
	}
	assertDeadlineWithin(t, runner.deadlines, caWallTimeout)
}
