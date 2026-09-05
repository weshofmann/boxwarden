// Package guestproto implements the deliberately small guest-side bootstrap
// protocol. It never receives host private trust material.
package guestproto

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

const (
	Version          = 1
	MaxRequestBytes  = 64 << 10
	MaxResponseBytes = 64 << 10
)

type Association struct {
	Domain        string `json:"domain"`
	SessionID     string `json:"session_id"`
	BackendKind   string `json:"backend_kind"`
	BackendObject string `json:"backend_object"`
}
type SerialRequest struct {
	Version         int    `json:"version"`
	Nonce           string `json:"nonce"`
	StartGeneration string `json:"start_generation"`
	Association
	CAPublicKey   string `json:"ca_public_key"`
	CAFingerprint string `json:"ca_fingerprint"`
	Principal     string `json:"principal"`
}
type ManagementRequest struct {
	Version int    `json:"version"`
	Kind    string `json:"kind"`
	Association
	Zone string `json:"zone,omitempty"`
}
type SerialResult struct {
	Version         int    `json:"version"`
	StartGeneration string `json:"start_generation"`
	Association
	CAFingerprint   string            `json:"ca_fingerprint"`
	Principal       string            `json:"principal"`
	InstalledSHA256 map[string]string `json:"installed_sha256"`
	SSHD            map[string]string `json:"sshd"`
	HostPublicKey   string            `json:"host_public_key"`
}

func DecodeSerialRequest(reader io.Reader) (SerialRequest, error) {
	contents, err := readBounded(reader, MaxRequestBytes)
	if err != nil {
		return SerialRequest{}, err
	}
	fields, err := exactObject(contents, "version", "nonce", "start_generation", "domain", "session_id", "backend_kind", "backend_object", "ca_public_key", "ca_fingerprint", "principal")
	if err != nil {
		return SerialRequest{}, err
	}
	var value SerialRequest
	if err := decodeFields(fields, &value); err != nil {
		return SerialRequest{}, err
	}
	if err := value.Validate(); err != nil {
		return SerialRequest{}, err
	}
	return value, nil
}
func DecodeManagementRequest(reader io.Reader) (ManagementRequest, error) {
	contents, err := readBounded(reader, MaxRequestBytes)
	if err != nil {
		return ManagementRequest{}, err
	}
	fields, err := exactObject(contents, "version", "kind", "domain", "session_id", "backend_kind", "backend_object", "zone")
	if err != nil {
		return ManagementRequest{}, err
	}
	var value ManagementRequest
	if err := decodeFields(fields, &value); err != nil {
		return ManagementRequest{}, err
	}
	if err := value.Validate(); err != nil {
		return ManagementRequest{}, err
	}
	return value, nil
}
func (r SerialRequest) Validate() error {
	if r.Version != Version || !validToken(r.Nonce, 1, 128) || !validUUID(r.StartGeneration) || !r.Association.valid() || !validPublicKey(r.CAPublicKey) || !validFingerprint(r.CAFingerprint) || r.Principal != derivedPrincipal(r.SessionID) {
		return fmt.Errorf("invalid serial bootstrap request")
	}
	if fingerprint(r.CAPublicKey) != r.CAFingerprint {
		return fmt.Errorf("CA fingerprint does not match public key")
	}
	return nil
}
func (r ManagementRequest) Validate() error {
	if r.Version != Version || !r.Association.valid() {
		return fmt.Errorf("invalid management request")
	}
	switch r.Kind {
	case "probe", "read_zone":
		if r.Zone != "" {
			return fmt.Errorf("management request has unexpected zone")
		}
	case "apply_zone":
		if !validZone(r.Zone) {
			return fmt.Errorf("invalid time zone")
		}
	default:
		return fmt.Errorf("unsupported management request")
	}
	return nil
}
func (a Association) valid() bool {
	return validToken(a.Domain, 1, 63) && validUUID(a.SessionID) && validToken(a.BackendKind, 1, 63) && validToken(a.BackendObject, 1, 255)
}
func derivedPrincipal(session string) string { return "boxwarden-session-" + session }
func validToken(value string, minimum, maximum int) bool {
	if len(value) < minimum || len(value) > maximum {
		return false
	}
	for _, r := range value {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.' || r == ':') {
			return false
		}
	}
	return true
}
func validUUID(value string) bool {
	if len(value) != 36 {
		return false
	}
	for i, r := range value {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if r != '-' {
				return false
			}
			continue
		}
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
			return false
		}
	}
	return true
}
func validZone(value string) bool {
	if len(value) == 0 || len(value) > 127 || strings.Contains(value, "..") {
		return false
	}
	for _, r := range value {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '/' || r == '_' || r == '-' || r == '+') {
			return false
		}
	}
	return true
}
func validPublicKey(value string) bool {
	p := strings.Fields(value)
	if len(p) != 2 || p[0] != "ssh-ed25519" {
		return false
	}
	raw, err := base64.StdEncoding.DecodeString(p[1])
	return err == nil && len(raw) == 51
}
func fingerprint(value string) string {
	parts := strings.Fields(value)
	if len(parts) != 2 {
		return ""
	}
	key, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(key)
	return "SHA256:" + base64.RawStdEncoding.EncodeToString(sum[:])
}
func validFingerprint(value string) bool {
	return strings.HasPrefix(value, "SHA256:") && len(value) == 50
}
func readBounded(reader io.Reader, limit int) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(reader, int64(limit)+1))
	if err != nil {
		return nil, err
	}
	if len(data) > limit {
		return nil, fmt.Errorf("request exceeds %d bytes", limit)
	}
	return data, nil
}
func exactObject(data []byte, allowed ...string) (map[string]json.RawMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return nil, fmt.Errorf("request must be a JSON object")
	}
	permitted := map[string]bool{}
	for _, key := range allowed {
		permitted[key] = true
	}
	fields := map[string]json.RawMessage{}
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		key, ok := token.(string)
		if !ok || !permitted[key] || fields[key] != nil {
			return nil, fmt.Errorf("invalid or duplicate field")
		}
		var raw json.RawMessage
		if err := decoder.Decode(&raw); err != nil {
			return nil, err
		}
		fields[key] = raw
	}
	if _, err := decoder.Token(); err != nil {
		return nil, err
	}
	if decoder.More() {
		return nil, fmt.Errorf("trailing JSON")
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return nil, fmt.Errorf("trailing JSON")
	}
	for _, key := range allowed {
		if fields[key] == nil && key != "zone" {
			return nil, fmt.Errorf("missing field %q", key)
		}
	}
	return fields, nil
}
func decodeFields(fields map[string]json.RawMessage, value any) error {
	raw, err := json.Marshal(fields)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, value)
}
func EncodeSerialFrame(request SerialRequest, result SerialResult) (string, string, error) {
	if result.StartGeneration != request.StartGeneration {
		return "", "", fmt.Errorf("result start generation differs from request")
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return "", "", err
	}
	if len(encoded) > MaxResponseBytes {
		return "", "", fmt.Errorf("response exceeds bound")
	}
	return fmt.Sprintf("BOXWARDEN-BEGIN %s %s", request.Nonce, request.SessionID), fmt.Sprintf("BOXWARDEN-END %s %s %s", request.Nonce, request.SessionID, base64.StdEncoding.EncodeToString(encoded)), nil
}
