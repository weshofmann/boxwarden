package guestproto

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"strings"
	"unicode"
)

const (
	MaxActionRequestBytes = 64 << 10
	MaxActionReceiptBytes = 4 << 10
)

// ActionRequest is one fixed guest-only command intent. The trusted host
// derives it from a re-admitted stored recipe and a durable attempt; the guest
// checks its installed association before execution. No field is a host path.
type ActionRequest struct {
	Version int `json:"version"`
	Association
	Generation   string   `json:"generation"`
	RecipeDigest string   `json:"recipe_digest"`
	ActionID     string   `json:"action_id"`
	ActionPhase  string   `json:"action_phase"`
	AttemptID    string   `json:"attempt_id"`
	Argv         []string `json:"argv"`
}

// ActionReceipt reports only guest cooperation. Even an exact receipt cannot
// establish trust in software running inside a disposable guest.
type ActionReceipt struct {
	Version int `json:"version"`
	Association
	Generation    string `json:"generation"`
	RecipeDigest  string `json:"recipe_digest"`
	ActionID      string `json:"action_id"`
	ActionPhase   string `json:"action_phase"`
	AttemptID     string `json:"attempt_id"`
	RequestSHA256 string `json:"request_sha256"`
	State         string `json:"state"`
}

func (r ActionRequest) Validate() error {
	if r.Version != Version || !r.Association.valid() || r.BackendKind != "tart" ||
		!validUUID(r.Generation) || !validUUID(r.AttemptID) ||
		r.AttemptID == "00000000-0000-0000-0000-000000000000" ||
		!actionSHA256(r.RecipeDigest) || !actionID(r.ActionID) {
		return fmt.Errorf("invalid action request binding")
	}
	switch r.ActionPhase {
	case "once", "reconfigure", "startup", "launch":
	default:
		return fmt.Errorf("invalid action phase")
	}
	if len(r.Argv) < 1 || len(r.Argv) > 32 || !path.IsAbs(r.Argv[0]) ||
		path.Clean(r.Argv[0]) != r.Argv[0] || r.Argv[0] == "/" {
		return fmt.Errorf("invalid action executable")
	}
	for index, arg := range r.Argv {
		if len(arg) > 65536 || strings.IndexFunc(arg, func(c rune) bool {
			return unicode.IsControl(c) && c != '\n' && c != '\t'
		}) >= 0 {
			return fmt.Errorf("invalid action argument")
		}
		if index == 0 && strings.IndexFunc(arg, unicode.IsControl) >= 0 {
			return fmt.Errorf("invalid action executable")
		}
	}
	return nil
}

func actionID(value string) bool {
	if len(value) < 1 || len(value) > 64 || value[0] < 'a' || value[0] > 'z' {
		return false
	}
	for _, c := range value[1:] {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
			return false
		}
	}
	return true
}

func actionSHA256(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, c := range value {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

func EncodeActionRequest(request ActionRequest) ([]byte, string, error) {
	if err := request.Validate(); err != nil {
		return nil, "", err
	}
	raw, err := json.Marshal(request)
	if err != nil || len(raw) > MaxActionRequestBytes {
		return nil, "", fmt.Errorf("encode bounded action request: %v", err)
	}
	sum := sha256.Sum256(raw)
	return raw, hex.EncodeToString(sum[:]), nil
}

func DecodeActionRequest(reader io.Reader) (ActionRequest, error) {
	raw, err := readBounded(reader, MaxActionRequestBytes)
	if err != nil {
		return ActionRequest{}, err
	}
	fields, err := exactObject(raw, "version", "domain", "session_id", "backend_kind", "backend_object", "generation", "recipe_digest", "action_id", "action_phase", "attempt_id", "argv")
	if err != nil {
		return ActionRequest{}, err
	}
	var request ActionRequest
	if err := decodeFields(fields, &request); err != nil {
		return ActionRequest{}, err
	}
	if err := request.Validate(); err != nil {
		return ActionRequest{}, err
	}
	canonical, _, err := EncodeActionRequest(request)
	if err != nil || !bytes.Equal(raw, canonical) {
		return ActionRequest{}, fmt.Errorf("action request is not canonical")
	}
	return request, nil
}

func (receipt ActionReceipt) validateFor(request ActionRequest) error {
	if err := request.Validate(); err != nil {
		return err
	}
	_, digest, err := EncodeActionRequest(request)
	if err != nil {
		return err
	}
	if receipt.Version != Version || receipt.Association != request.Association ||
		receipt.Generation != request.Generation ||
		receipt.RecipeDigest != request.RecipeDigest ||
		receipt.ActionID != request.ActionID ||
		receipt.ActionPhase != request.ActionPhase ||
		receipt.AttemptID != request.AttemptID ||
		receipt.RequestSHA256 != digest || receipt.State != "succeeded" {
		return fmt.Errorf("action receipt differs from exact request")
	}
	return nil
}

func EncodeActionReceipt(request ActionRequest, receipt ActionReceipt) ([]byte, error) {
	if err := receipt.validateFor(request); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(receipt)
	if err != nil || len(raw) > MaxActionReceiptBytes {
		return nil, fmt.Errorf("encode bounded action receipt: %v", err)
	}
	return raw, nil
}

func DecodeActionReceipt(request ActionRequest, reader io.Reader) (ActionReceipt, error) {
	raw, err := readBounded(reader, MaxActionReceiptBytes)
	if err != nil {
		return ActionReceipt{}, err
	}
	raw = bytes.TrimSuffix(raw, []byte("\n"))
	fields, err := exactObject(raw, "version", "domain", "session_id", "backend_kind", "backend_object", "generation", "recipe_digest", "action_id", "action_phase", "attempt_id", "request_sha256", "state")
	if err != nil {
		return ActionReceipt{}, err
	}
	var receipt ActionReceipt
	if err := decodeFields(fields, &receipt); err != nil {
		return ActionReceipt{}, err
	}
	if err := receipt.validateFor(request); err != nil {
		return ActionReceipt{}, err
	}
	canonical, err := EncodeActionReceipt(request, receipt)
	if err != nil || !bytes.Equal(raw, canonical) {
		return ActionReceipt{}, fmt.Errorf("action receipt is not canonical")
	}
	return receipt, nil
}
