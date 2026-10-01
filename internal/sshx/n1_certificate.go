//go:build n1clipboarddiagnostic && !n1candidate

package sshx

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"strings"
	"time"
)

type N1CertificateObservation struct {
	SHA256        string `json:"sha256"`
	Fingerprint   string `json:"fingerprint"`
	CAFingerprint string `json:"ca_fingerprint"`
	Principal     string `json:"principal"`
	Identity      string `json:"identity"`
	NotBefore     uint64 `json:"not_before"`
	NotAfter      uint64 `json:"not_after"`
}
type n1Wire struct {
	b      []byte
	offset int
	bad    bool
}

func (w *n1Wire) take(n int) []byte {
	if w.bad || n < 0 || n > len(w.b)-w.offset {
		w.bad = true
		return nil
	}
	v := w.b[w.offset : w.offset+n]
	w.offset += n
	return v
}
func (w *n1Wire) u32() uint32 {
	b := w.take(4)
	if len(b) != 4 {
		return 0
	}
	return binary.BigEndian.Uint32(b)
}
func (w *n1Wire) u64() uint64 {
	b := w.take(8)
	if len(b) != 8 {
		return 0
	}
	return binary.BigEndian.Uint64(b)
}
func (w *n1Wire) str() []byte {
	n := w.u32()
	if n > 65536 {
		w.bad = true
		return nil
	}
	return w.take(int(n))
}
func (w *n1Wire) done() bool { return !w.bad && w.offset == len(w.b) }
func n1PublicWire(s string) ([]byte, []byte, bool) {
	public, _, _, e := parseEd25519PublicKey(s)
	if e != nil {
		return nil, nil, false
	}
	parts := strings.Fields(public)
	if len(parts) != 2 {
		return nil, nil, false
	}
	raw, e := base64.StdEncoding.Strict().DecodeString(parts[1])
	if e != nil {
		return nil, nil, false
	}
	w := n1Wire{b: raw}
	kind, key := w.str(), w.str()
	return raw, key, w.done() && string(kind) == "ssh-ed25519" && len(key) == ed25519.PublicKeySize
}

// Admission reads only public files; private keys receive metadata checks through
// the existing connection policy. It never issues/renews or reads a private CA.
func InspectN1Certificate(conn Connection, caPublic string, now time.Time) (N1CertificateObservation, error) {
	if validateConnection(conn) != nil || verifyKnownHostsPin(conn) != nil {
		return N1CertificateObservation{}, ErrN1Guest
	}
	raw, e := readRuntimeFile(conn.RuntimeDirectory, conn.CertificateFile, publicFileMode)
	if e != nil {
		return N1CertificateObservation{}, ErrN1Guest
	}
	client, e := readRuntimeFile(conn.RuntimeDirectory, conn.IdentityFile+".pub", publicFileMode)
	if e != nil {
		return N1CertificateObservation{}, ErrN1Guest
	}
	parts := strings.Fields(string(raw))
	if len(parts) < 2 || len(parts) > 3 || parts[0] != "ssh-ed25519-cert-v01@openssh.com" {
		return N1CertificateObservation{}, ErrN1Guest
	}
	wire, e := base64.StdEncoding.Strict().DecodeString(parts[1])
	if e != nil {
		return N1CertificateObservation{}, ErrN1Guest
	}
	v, e := admitN1Certificate(wire, caPublic, string(client), conn.Binding, now)
	if e != nil {
		return v, e
	}
	after, e := readRuntimeFile(conn.RuntimeDirectory, conn.CertificateFile, publicFileMode)
	clientAfter, clientError := readRuntimeFile(conn.RuntimeDirectory, conn.IdentityFile+".pub", publicFileMode)
	if e != nil || clientError != nil || !bytes.Equal(raw, after) || !bytes.Equal(client, clientAfter) || validateConnection(conn) != nil || verifyKnownHostsPin(conn) != nil {
		return N1CertificateObservation{}, ErrN1Guest
	}
	h := sha256.Sum256(raw)
	v.SHA256 = hex.EncodeToString(h[:])
	return v, nil
}
func admitN1Certificate(raw []byte, caPublic, clientPublic string, b Binding, now time.Time) (N1CertificateObservation, error) {
	refuse := func() (N1CertificateObservation, error) { return N1CertificateObservation{}, ErrN1Guest }
	if len(raw) == 0 || len(raw) > 65536 || b.Validate() != nil || string(b.Domain) != "n1qualification" || b.BackendKind != "tart" || now.Unix() < 0 {
		return refuse()
	}
	caWire, ca, ok := n1PublicWire(caPublic)
	if !ok {
		return refuse()
	}
	_, client, ok := n1PublicWire(clientPublic)
	if !ok {
		return refuse()
	}
	w := n1Wire{b: raw}
	kind, nonce, key := w.str(), w.str(), w.str()
	w.u64()
	typ := w.u32()
	id := w.str()
	principals := n1Wire{b: w.str()}
	principal := principals.str()
	before, after := w.u64(), w.u64()
	critical, extensions, reserved := w.str(), w.str(), w.str()
	signer := w.str()
	signed := w.offset
	signature := n1Wire{b: w.str()}
	algorithm, sig := signature.str(), signature.str()
	if !w.done() || !principals.done() || !signature.done() || string(kind) != "ssh-ed25519-cert-v01@openssh.com" || len(nonce) == 0 || !bytes.Equal(key, client) || typ != 1 || string(id) != b.CertificateIdentity() || string(principal) != b.Principal() || after <= before || after-before != 1200 || uint64(now.Unix()) < before || uint64(now.Unix()) >= after || len(critical) != 0 || len(extensions) != 0 || len(reserved) != 0 || !bytes.Equal(signer, caWire) || string(algorithm) != "ssh-ed25519" || !ed25519.Verify(ca, raw[:signed], sig) {
		return refuse()
	}
	h, ch := sha256.Sum256(raw), sha256.Sum256(caWire)
	return N1CertificateObservation{Fingerprint: "SHA256:" + base64.RawStdEncoding.EncodeToString(h[:]), CAFingerprint: "SHA256:" + base64.RawStdEncoding.EncodeToString(ch[:]), Principal: string(principal), Identity: string(id), NotBefore: before, NotAfter: after}, nil
}
