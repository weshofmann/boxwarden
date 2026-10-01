//go:build n1clipboarddiagnostic && !n1candidate

package sshx

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"testing"
	"time"
)

func sshString(b []byte) []byte {
	out := make([]byte, 4)
	binary.BigEndian.PutUint32(out, uint32(len(b)))
	return append(out, b...)
}
func sshU64(n uint64) []byte { b := make([]byte, 8); binary.BigEndian.PutUint64(b, n); return b }
func certificateFixture(t *testing.T, principal, keyid string, options []byte) ([]byte, string, string) {
	t.Helper()
	ca, priv, e := ed25519.GenerateKey(rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	client, _, e := ed25519.GenerateKey(rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	caWire := append(sshString([]byte("ssh-ed25519")), sshString(ca)...)
	clientWire := append(sshString([]byte("ssh-ed25519")), sshString(client)...)
	raw := append(sshString([]byte("ssh-ed25519-cert-v01@openssh.com")), sshString([]byte("nonce"))...)
	raw = append(raw, sshString(client)...)
	raw = append(raw, sshU64(1)...)
	raw = append(raw, 0, 0, 0, 1)
	raw = append(raw, sshString([]byte(keyid))...)
	raw = append(raw, sshString(sshString([]byte(principal)))...)
	raw = append(raw, sshU64(1000)...)
	raw = append(raw, sshU64(2200)...)
	raw = append(raw, sshString(options)...)
	raw = append(raw, sshString(nil)...)
	raw = append(raw, sshString(nil)...)
	raw = append(raw, sshString(caWire)...)
	sig := ed25519.Sign(priv, raw)
	sigWire := append(sshString([]byte("ssh-ed25519")), sshString(sig)...)
	raw = append(raw, sshString(sigWire)...)
	return raw, "ssh-ed25519 " + base64.StdEncoding.EncodeToString(caWire), "ssh-ed25519 " + base64.StdEncoding.EncodeToString(clientWire)
}
func TestN1PublicCertificateSignedExactPolicy(t *testing.T) {
	b := Binding{Domain: "n1qualification", SessionID: "11111111-1111-4111-8111-111111111111", BackendKind: "tart", BackendObject: "boxwarden-x"}
	raw, ca, client := certificateFixture(t, b.Principal(), b.CertificateIdentity(), nil)
	if _, e := admitN1Certificate(raw, ca, client, b, time.Unix(1500, 0)); e != nil {
		t.Fatal(e)
	}
	for _, now := range []int64{999, 2200} {
		if _, e := admitN1Certificate(raw, ca, client, b, time.Unix(now, 0)); e == nil {
			t.Fatal("expired accepted")
		}
	}
	for _, bad := range [][]byte{append(append([]byte{}, raw...), 0), raw[:len(raw)-1], append([]byte{}, raw...)} {
		if len(bad) == len(raw) {
			bad[len(bad)-1] ^= 1
		}
		if _, e := admitN1Certificate(bad, ca, client, b, time.Unix(1500, 0)); e == nil {
			t.Fatal("malformed/signature accepted")
		}
	}
}
func TestN1PublicCertificateForeignPrincipalAndExtensions(t *testing.T) {
	b := Binding{Domain: "n1qualification", SessionID: "11111111-1111-4111-8111-111111111111", BackendKind: "tart", BackendObject: "boxwarden-x"}
	for _, x := range []struct {
		p, id   string
		options []byte
	}{{"foreign", b.CertificateIdentity(), nil}, {b.Principal(), "foreign", nil}, {b.Principal(), b.CertificateIdentity(), []byte("permit-any")}} {
		raw, ca, client := certificateFixture(t, x.p, x.id, x.options)
		if _, e := admitN1Certificate(raw, ca, client, b, time.Unix(1500, 0)); e == nil {
			t.Fatal("foreign certificate accepted")
		}
	}
}

func TestN1PublicCertificateMalformedTrustAndLengthControls(t *testing.T) {
	b := Binding{Domain: "n1qualification", SessionID: "11111111-1111-4111-8111-111111111111", BackendKind: "tart", BackendObject: "boxwarden-x"}
	raw, ca, client := certificateFixture(t, b.Principal(), b.CertificateIdentity(), nil)
	_, foreignCA, foreignClient := certificateFixture(t, b.Principal(), b.CertificateIdentity(), nil)
	for name, x := range map[string]struct {
		raw        []byte
		ca, client string
	}{
		"CA": {raw, foreignCA, client}, "client": {raw, ca, foreignClient}, "overflow": {[]byte{255, 255, 255, 255}, ca, client}, "too-large": {make([]byte, 65537), ca, client}, "duplicate-certificate": {append(append([]byte{}, raw...), raw...), ca, client},
	} {
		if _, e := admitN1Certificate(x.raw, x.ca, x.client, b, time.Unix(1500, 0)); e == nil {
			t.Fatal("malformed public certificate accepted", name)
		}
	}
	// Re-sign every policy mutation so refusal cannot merely come from a stale signature.
	for _, kind := range []string{"duplicate-principal", "extensions", "reserved", "wrong-validity", "overflow-validity", "host-type", "wrong-algorithm"} {
		bad, ca, client := signedPolicyMutation(t, b, kind)
		if _, e := admitN1Certificate(bad, ca, client, b, time.Unix(1500, 0)); e == nil {
			t.Fatal("signed forbidden policy accepted", kind)
		}
	}
}
func signedPolicyMutation(t *testing.T, b Binding, kind string) ([]byte, string, string) {
	t.Helper()
	ca, private, e := ed25519.GenerateKey(rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	client, _, e := ed25519.GenerateKey(rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	caWire := append(sshString([]byte("ssh-ed25519")), sshString(ca)...)
	clientWire := append(sshString([]byte("ssh-ed25519")), sshString(client)...)
	raw := append(sshString([]byte("ssh-ed25519-cert-v01@openssh.com")), sshString([]byte("nonce"))...)
	raw = append(raw, sshString(client)...)
	raw = append(raw, sshU64(1)...)
	typ := byte(1)
	if kind == "host-type" {
		typ = 2
	}
	raw = append(raw, 0, 0, 0, typ)
	raw = append(raw, sshString([]byte(b.CertificateIdentity()))...)
	principals := sshString([]byte(b.Principal()))
	if kind == "duplicate-principal" {
		principals = append(principals, sshString([]byte(b.Principal()))...)
	}
	raw = append(raw, sshString(principals)...)
	before, after := uint64(1000), uint64(2200)
	if kind == "wrong-validity" {
		after++
	}
	if kind == "overflow-validity" {
		before = ^uint64(0) - 100
		after = 99
	}
	raw = append(raw, sshU64(before)...)
	raw = append(raw, sshU64(after)...)
	raw = append(raw, sshString(nil)...)
	extensions, reserved := []byte(nil), []byte(nil)
	if kind == "extensions" {
		extensions = sshString([]byte("permit-pty"))
	}
	if kind == "reserved" {
		reserved = []byte("reserved")
	}
	raw = append(raw, sshString(extensions)...)
	raw = append(raw, sshString(reserved)...)
	raw = append(raw, sshString(caWire)...)
	algorithm := "ssh-ed25519"
	if kind == "wrong-algorithm" {
		algorithm = "ssh-rsa"
	}
	sig := append(sshString([]byte(algorithm)), sshString(ed25519.Sign(private, raw))...)
	raw = append(raw, sshString(sig)...)
	return raw, "ssh-ed25519 " + base64.StdEncoding.EncodeToString(caWire), "ssh-ed25519 " + base64.StdEncoding.EncodeToString(clientWire)
}
