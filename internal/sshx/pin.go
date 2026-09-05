package sshx

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/weshofmann/boxwarden/internal/domain"
)

const hostKeyPinVersion = 1

type ObservedHostKey struct {
	Algorithm string
	PublicKey string
}

// HostKeyPin deliberately records no network address: the durable identity is the binding and exact key.
type HostKeyPin struct {
	Version       int       `json:"version"`
	Domain        domain.ID `json:"domain"`
	SessionID     string    `json:"session_id"`
	BackendKind   string    `json:"backend_kind"`
	BackendObject string    `json:"backend_object"`
	Algorithm     string    `json:"algorithm"`
	PublicKey     string    `json:"public_key"`
	Fingerprint   string    `json:"fingerprint"`
}

type PinStore struct{ domain Domain }

func NewPinStore(value Domain) *PinStore { return &PinStore{domain: value} }

func (s *PinStore) Admit(_ context.Context, binding Binding, observed ObservedHostKey) (HostKeyPin, error) {
	if s == nil {
		return HostKeyPin{}, fmt.Errorf("pin store is required")
	}
	if err := validDomain(s.domain); err != nil {
		return HostKeyPin{}, err
	}
	if err := binding.Validate(); err != nil {
		return HostKeyPin{}, err
	}
	if binding.Domain != s.domain.ID {
		return HostKeyPin{}, fmt.Errorf("host-key pin binding domain does not match pin-store domain")
	}
	if observed.Algorithm != "ssh-ed25519" {
		return HostKeyPin{}, fmt.Errorf("host key algorithm must be ssh-ed25519")
	}
	public, _, fingerprint, err := parseEd25519PublicKey(observed.PublicKey)
	if err != nil {
		return HostKeyPin{}, fmt.Errorf("invalid observed host key: %w", err)
	}
	pin := HostKeyPin{Version: hostKeyPinVersion, Domain: binding.Domain, SessionID: binding.SessionID, BackendKind: binding.BackendKind, BackendObject: binding.BackendObject, Algorithm: observed.Algorithm, PublicKey: public, Fingerprint: fingerprint}
	if err := ensurePrivateDirectory(s.domain.StateRoot); err != nil {
		return HostKeyPin{}, err
	}
	if err := ensurePrivateDirectory(filepath.Join(s.domain.StateRoot, "identity")); err != nil {
		return HostKeyPin{}, err
	}
	dir := pinDirectory(s.domain.StateRoot)
	if err := ensurePrivateDirectory(dir); err != nil {
		return HostKeyPin{}, err
	}
	path := filepath.Join(dir, binding.SessionID+".json")
	if _, err := os.Lstat(path); err == nil {
		existing, err := s.Load(context.Background(), binding)
		if err != nil {
			return HostKeyPin{}, err
		}
		if existing != pin {
			return HostKeyPin{}, fmt.Errorf("existing host-key pin differs from observed key or binding")
		}
		return existing, nil
	} else if !os.IsNotExist(err) {
		return HostKeyPin{}, err
	}
	encoded, err := json.Marshal(pin)
	if err != nil {
		return HostKeyPin{}, err
	}
	if err := writePrivateNew(path, append(encoded, '\n')); err != nil {
		if !os.IsExist(err) {
			return HostKeyPin{}, fmt.Errorf("persist host-key pin: %w", err)
		}
		existing, loadErr := s.Load(context.Background(), binding)
		if loadErr != nil || existing != pin {
			return HostKeyPin{}, fmt.Errorf("concurrent or conflicting host-key pin admission")
		}
	}
	return pin, nil
}

func (s *PinStore) Load(_ context.Context, binding Binding) (HostKeyPin, error) {
	if s == nil {
		return HostKeyPin{}, fmt.Errorf("pin store is required")
	}
	if err := validDomain(s.domain); err != nil {
		return HostKeyPin{}, err
	}
	if err := binding.Validate(); err != nil {
		return HostKeyPin{}, err
	}
	if binding.Domain != s.domain.ID {
		return HostKeyPin{}, fmt.Errorf("host-key pin binding domain does not match pin-store domain")
	}
	if err := requirePrivateTree(s.domain.StateRoot, pinDirectory(s.domain.StateRoot)); err != nil {
		return HostKeyPin{}, fmt.Errorf("host-key pin directory: %w", err)
	}
	entries, err := os.ReadDir(pinDirectory(s.domain.StateRoot))
	if err != nil {
		return HostKeyPin{}, err
	}
	for _, entry := range entries {
		name := entry.Name()
		if len(name) != 41 || filepath.Ext(name) != ".json" || !validUUID(name[:len(name)-5]) {
			return HostKeyPin{}, fmt.Errorf("unexpected host-key pin entry %q", name)
		}
	}
	contents, err := readPrivateFile(filepath.Join(pinDirectory(s.domain.StateRoot), binding.SessionID+".json"))
	if err != nil {
		return HostKeyPin{}, fmt.Errorf("read host-key pin: %w", err)
	}
	pin, err := decodeHostKeyPin(contents)
	if err != nil {
		return HostKeyPin{}, fmt.Errorf("parse host-key pin: %w", err)
	}
	if pin.Version != hostKeyPinVersion || pin.Domain != binding.Domain || pin.SessionID != binding.SessionID || pin.BackendKind != binding.BackendKind || pin.BackendObject != binding.BackendObject || pin.Algorithm != "ssh-ed25519" {
		return HostKeyPin{}, fmt.Errorf("host-key pin binding is invalid")
	}
	public, _, fingerprint, err := parseEd25519PublicKey(pin.PublicKey)
	if err != nil || public != pin.PublicKey || fingerprint != pin.Fingerprint {
		return HostKeyPin{}, fmt.Errorf("host-key pin key material is invalid")
	}
	return pin, nil
}

func decodeHostKeyPin(contents []byte) (HostKeyPin, error) {
	fields, err := decodeExactObject(contents, "version", "domain", "session_id", "backend_kind", "backend_object", "algorithm", "public_key", "fingerprint")
	if err != nil {
		return HostKeyPin{}, err
	}
	var pin HostKeyPin
	var rawDomain string
	for name, target := range map[string]any{"version": &pin.Version, "domain": &rawDomain, "session_id": &pin.SessionID, "backend_kind": &pin.BackendKind, "backend_object": &pin.BackendObject, "algorithm": &pin.Algorithm, "public_key": &pin.PublicKey, "fingerprint": &pin.Fingerprint} {
		if err := decodeField(fields, name, target); err != nil {
			return HostKeyPin{}, err
		}
	}
	pin.Domain = domain.ID(rawDomain)
	return pin, nil
}
