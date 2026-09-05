// Package guestproto implements the deliberately small guest-side bootstrap
// protocol. It never receives host private trust material.
package guestproto

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

const (
	Version          = 1
	MaxRequestBytes  = 64 << 10
	MaxResponseBytes = 64 << 10
	maxNonceBytes    = 128
)

var requiredInstalledSHA256 = map[string]struct{}{
	"trusted-user-ca.pub":             {},
	"authorized_principals/boxwarden": {},
	"management-binding.json":         {},
}

var requiredSSHD = map[string]string{
	"trustedusercakeys":            "/etc/ssh/boxwarden/active/trusted-user-ca.pub",
	"authorizedprincipalsfile":     "/etc/ssh/boxwarden/active/authorized_principals/%u",
	"authorizedkeysfile":           "none",
	"permituserenvironment":        "no",
	"permituserrc":                 "no",
	"passwordauthentication":       "no",
	"kbdinteractiveauthentication": "no",
	"permitrootlogin":              "no",
	"x11forwarding":                "no",
	"allowagentforwarding":         "no",
	"allowtcpforwarding":           "no",
	"allowstreamlocalforwarding":   "no",
	"gatewayports":                 "no",
	"permittunnel":                 "no",
}

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
	if err != nil || base64.StdEncoding.EncodeToString(raw) != p[1] {
		return false
	}
	innerType, rest, ok := readRFC4253String(raw)
	if !ok || string(innerType) != "ssh-ed25519" {
		return false
	}
	key, rest, ok := readRFC4253String(rest)
	return ok && len(key) == 32 && len(rest) == 0
}

func readRFC4253String(input []byte) ([]byte, []byte, bool) {
	if len(input) < 4 {
		return nil, nil, false
	}
	length := binary.BigEndian.Uint32(input[:4])
	if length > uint32(len(input)-4) {
		return nil, nil, false
	}
	return input[4 : 4+length], input[4+length:], true
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
	if err := validateSerialResult(request, result); err != nil {
		return "", "", err
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

// DecodeSerialEndLine accepts exactly one optional CR produced by a PTY at the
// line boundary. It does not normalize payload bytes or otherwise loosen the
// canonical serial frame grammar.
func DecodeSerialEndLine(request SerialRequest, line string) (SerialResult, error) {
	if len(line) > maxSerialEndLineBytes() {
		return SerialResult{}, fmt.Errorf("serial end frame exceeds bound")
	}
	if strings.HasSuffix(line, "\r") {
		line = strings.TrimSuffix(line, "\r")
	}
	if strings.ContainsAny(line, "\r\n") {
		return SerialResult{}, fmt.Errorf("invalid serial frame line ending")
	}
	fields := strings.Split(line, " ")
	if len(fields) != 4 || fields[0] != "BOXWARDEN-END" || fields[1] != request.Nonce || fields[2] != request.SessionID {
		return SerialResult{}, fmt.Errorf("invalid serial end frame")
	}
	decodedLength, ok := encodedDecodedLength(fields[3])
	if !ok || decodedLength > MaxResponseBytes {
		return SerialResult{}, fmt.Errorf("serial end payload exceeds bound")
	}
	decoded, err := base64.StdEncoding.DecodeString(fields[3])
	if err != nil || base64.StdEncoding.EncodeToString(decoded) != fields[3] {
		return SerialResult{}, fmt.Errorf("invalid serial end payload")
	}
	if len(decoded) > MaxResponseBytes {
		return SerialResult{}, fmt.Errorf("serial end payload exceeds bound")
	}
	object, err := exactObject(decoded, "version", "start_generation", "domain", "session_id", "backend_kind", "backend_object", "ca_fingerprint", "principal", "installed_sha256", "sshd", "host_public_key")
	if err != nil {
		return SerialResult{}, fmt.Errorf("invalid serial result: %w", err)
	}
	digests, err := decodeExactDigests(object["installed_sha256"])
	if err != nil {
		return SerialResult{}, fmt.Errorf("invalid installed digests: %w", err)
	}
	sshd, err := decodeExactStringMap(object["sshd"], requiredSSHD)
	if err != nil {
		return SerialResult{}, fmt.Errorf("invalid effective sshd: %w", err)
	}
	var result SerialResult
	if err := decodeFields(object, &result); err != nil {
		return SerialResult{}, fmt.Errorf("invalid serial result: %w", err)
	}
	result.InstalledSHA256, result.SSHD = digests, sshd
	if err := validateSerialResult(request, result); err != nil {
		return SerialResult{}, err
	}
	return result, nil
}

func maxSerialEndLineBytes() int {
	return len("BOXWARDEN-END ") + maxNonceBytes + 1 + 36 + 1 + base64.StdEncoding.EncodedLen(MaxResponseBytes) + 1
}

func encodedDecodedLength(encoded string) (int, bool) {
	if len(encoded) == 0 || len(encoded)%4 != 0 {
		return 0, false
	}
	length := base64.StdEncoding.DecodedLen(len(encoded))
	if strings.HasSuffix(encoded, "==") {
		length -= 2
	} else if strings.HasSuffix(encoded, "=") {
		length--
	}
	return length, length >= 0
}

func decodeExactDigests(raw json.RawMessage) (map[string]string, error) {
	keys := make([]string, 0, len(requiredInstalledSHA256))
	for key := range requiredInstalledSHA256 {
		keys = append(keys, key)
	}
	fields, err := exactObject(raw, keys...)
	if err != nil {
		return nil, err
	}
	result := make(map[string]string, len(fields))
	for key, value := range fields {
		var digest string
		if err := json.Unmarshal(value, &digest); err != nil || !lowerHex64(digest) {
			return nil, fmt.Errorf("invalid digest %q", key)
		}
		result[key] = digest
	}
	return result, nil
}

func decodeExactStringMap(raw json.RawMessage, expected map[string]string) (map[string]string, error) {
	keys := make([]string, 0, len(expected))
	for key := range expected {
		keys = append(keys, key)
	}
	fields, err := exactObject(raw, keys...)
	if err != nil {
		return nil, err
	}
	result := make(map[string]string, len(fields))
	for key, want := range expected {
		var value string
		if err := json.Unmarshal(fields[key], &value); err != nil || value != want {
			return nil, fmt.Errorf("invalid value %q", key)
		}
		result[key] = value
	}
	return result, nil
}

func lowerHex64(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, r := range value {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}

func validateSerialResult(request SerialRequest, result SerialResult) error {
	if result.Version != Version || result.StartGeneration != request.StartGeneration || result.Association != request.Association || result.CAFingerprint != request.CAFingerprint || result.Principal != request.Principal {
		return fmt.Errorf("serial result does not match request")
	}
	if !validPublicKey(result.HostPublicKey) {
		return fmt.Errorf("invalid serial result host public key")
	}
	return nil
}
