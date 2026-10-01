package contract

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"strings"
)

type CAPublic struct {
	Version      int    `json:"version"`
	Domain       string `json:"domain"`
	Algorithm    string `json:"algorithm"`
	PublicKey    string `json:"public_key"`
	PublicDigest string `json:"public_digest"`
	Fingerprint  string `json:"fingerprint"`
	CreationUUID string `json:"creation_uuid"`
	CreatorUID   int    `json:"creator_uid"`
	CreatorName  string `json:"creator_name"`
}
type goldenBackend struct {
	Kind   string `json:"kind"`
	Object string `json:"object_id"`
}
type BaseRecord struct {
	Version  int           `json:"version"`
	Domain   string        `json:"domain"`
	Revision string        `json:"revision"`
	Backend  goldenBackend `json:"backend"`
}

func ParseCAPublic(raw []byte) (CAPublic, error) {
	var c CAPublic
	if len(raw) < 1 || raw[len(raw)-1] != '\n' || strict(raw[:len(raw)-1], 4096, &c) != nil || c.Version != 1 || c.Domain != "n1qualification" || c.Algorithm != "ssh-ed25519" || c.CreatorUID != 501 || c.CreatorName != "devel" || !UUID(c.CreationUUID) {
		return c, ErrRefused
	}
	parts := strings.Split(c.PublicKey, " ")
	if len(parts) != 2 || parts[0] != "ssh-ed25519" {
		return c, ErrRefused
	}
	wire, e := base64.StdEncoding.Strict().DecodeString(parts[1])
	if e != nil || len(wire) != 51 || binary.BigEndian.Uint32(wire[:4]) != 11 || string(wire[4:15]) != "ssh-ed25519" || binary.BigEndian.Uint32(wire[15:19]) != 32 || bytes.Equal(wire[19:], make([]byte, 32)) {
		return c, ErrRefused
	}
	sum := sha256.Sum256(wire)
	if c.PublicDigest != hex.EncodeToString(sum[:]) || c.Fingerprint != "SHA256:"+base64.RawStdEncoding.EncodeToString(sum[:]) {
		return c, ErrRefused
	}
	return c, nil
}
func ParseBase(raw []byte) (BaseRecord, error) {
	var b BaseRecord
	if len(raw) > 0 && raw[len(raw)-1] == '\n' {
		raw = raw[:len(raw)-1]
	}
	if strict(raw, 4096, &b) != nil || b.Version != 1 || b.Domain != "n1qualification" || b.Revision != BaseName || b.Backend.Kind != "tart" || b.Backend.Object != BaseName {
		return b, ErrRefused
	}
	return b, nil
}
func MatchPublicKey(raw []byte, c CAPublic) error {
	if len(raw) > 4096 || len(raw) == 0 || raw[len(raw)-1] != '\n' {
		return ErrRefused
	}
	s := string(raw[:len(raw)-1])
	if s != c.PublicKey && s != c.PublicKey+" boxwarden:n1qualification:management-ca" {
		return ErrRefused
	}
	return nil
}

type HostPin struct {
	Version       int    `json:"version"`
	Domain        string `json:"domain"`
	SessionID     string `json:"session_id"`
	BackendKind   string `json:"backend_kind"`
	BackendObject string `json:"backend_object"`
	Algorithm     string `json:"algorithm"`
	PublicKey     string `json:"public_key"`
	Fingerprint   string `json:"fingerprint"`
}

func ParseHostPin(raw []byte, peer Peer) (HostPin, error) {
	var p HostPin
	if len(raw) < 1 || raw[len(raw)-1] != '\n' || strict(raw[:len(raw)-1], 4096, &p) != nil || !digest(peer.HostPinSHA) || SHA(raw) != peer.HostPinSHA || p.Version != 1 || p.Domain != "n1qualification" || !UUID(peer.Session) || p.SessionID != peer.Session || p.BackendKind != "tart" || p.BackendObject != peer.Backend || p.Algorithm != "ssh-ed25519" {
		return p, ErrRefused
	}
	parts := strings.Split(p.PublicKey, " ")
	if len(parts) != 2 || parts[0] != "ssh-ed25519" {
		return p, ErrRefused
	}
	wire, e := base64.StdEncoding.Strict().DecodeString(parts[1])
	if e != nil || len(wire) != 51 || binary.BigEndian.Uint32(wire[:4]) != 11 || string(wire[4:15]) != "ssh-ed25519" || binary.BigEndian.Uint32(wire[15:19]) != 32 || bytes.Equal(wire[19:], make([]byte, 32)) {
		return p, ErrRefused
	}
	sum := sha256.Sum256(wire)
	if p.Fingerprint != "SHA256:"+base64.RawStdEncoding.EncodeToString(sum[:]) {
		return p, ErrRefused
	}
	return p, nil
}
