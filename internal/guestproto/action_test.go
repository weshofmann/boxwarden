package guestproto

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func testActionRequest() ActionRequest {
	return ActionRequest{
		Version: Version,
		Association: Association{
			Domain: "work", SessionID: "13b0bf73-3bd5-4f1c-8bdc-71d50c36d6d0",
			BackendKind: "tart", BackendObject: "boxwarden-work-dev",
		},
		Generation:   "00000000-0000-4000-8000-000000000003",
		RecipeDigest: strings.Repeat("a", 64),
		ActionID:     "configure-agent", ActionPhase: "once",
		AttemptID: "00112233-4455-4677-8899-aabbccddeeff",
		Argv:      []string{"/bin/bash", "-ec", "echo guest-only\n"},
	}
}

func TestActionRequestCanonicalRoundTripAndReceiptBinding(t *testing.T) {
	request := testActionRequest()
	raw, digest, err := EncodeActionRequest(request)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeActionRequest(bytes.NewReader(raw))
	if err != nil || decoded.ActionID != request.ActionID || decoded.Argv[2] != request.Argv[2] {
		t.Fatalf("decoded request = %#v, %v", decoded, err)
	}
	receipt := ActionReceipt{
		Version: Version, Association: request.Association,
		Generation: request.Generation, RecipeDigest: request.RecipeDigest,
		ActionID: request.ActionID, ActionPhase: request.ActionPhase,
		AttemptID: request.AttemptID, RequestSHA256: digest,
		State: "succeeded",
	}
	encoded, err := EncodeActionReceipt(request, receipt)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := DecodeActionReceipt(request, bytes.NewReader(encoded)); err != nil || got != receipt {
		t.Fatalf("decoded receipt = %#v, %v", got, err)
	}
	other := request
	other.Argv = []string{"/usr/bin/false"}
	if _, err := DecodeActionReceipt(other, bytes.NewReader(encoded)); err == nil {
		t.Fatal("receipt accepted for changed argv")
	}
}

func TestActionRequestRejectsMalformedOrAmbiguousInput(t *testing.T) {
	valid, _, err := EncodeActionRequest(testActionRequest())
	if err != nil {
		t.Fatal(err)
	}
	for label, raw := range map[string][]byte{
		"duplicate field":         bytes.Replace(valid, []byte(`"version":1`), []byte(`"version":1,"version":1`), 1),
		"unknown field":           append(append([]byte(nil), valid[:len(valid)-1]...), []byte(`,"extra":true}`)...),
		"noncanonical whitespace": append([]byte(" "), valid...),
		"trailing JSON":           append(append([]byte(nil), valid...), []byte(" {}")...),
	} {
		t.Run(label, func(t *testing.T) {
			if _, err := DecodeActionRequest(bytes.NewReader(raw)); err == nil {
				t.Fatal("ambiguous request accepted")
			}
		})
	}
	for label, mutate := range map[string]func(*ActionRequest){
		"relative program": func(r *ActionRequest) { r.Argv[0] = "bash" },
		"nul argument":     func(r *ActionRequest) { r.Argv[1] = "bad\x00arg" },
		"too many args":    func(r *ActionRequest) { r.Argv = make([]string, 33) },
		"bad action":       func(r *ActionRequest) { r.ActionID = "../escape" },
		"prepare phase":    func(r *ActionRequest) { r.ActionPhase = "prepare" },
		"wrong backend":    func(r *ActionRequest) { r.BackendKind = "docker" },
	} {
		t.Run(label, func(t *testing.T) {
			bad := testActionRequest()
			mutate(&bad)
			if _, _, err := EncodeActionRequest(bad); err == nil {
				t.Fatal("invalid action request encoded")
			}
		})
	}
}

func TestActionReceiptRejectsChangedIdentityAndFalseSuccess(t *testing.T) {
	request := testActionRequest()
	_, digest, err := EncodeActionRequest(request)
	if err != nil {
		t.Fatal(err)
	}
	receipt := ActionReceipt{
		Version: Version, Association: request.Association,
		Generation: request.Generation, RecipeDigest: request.RecipeDigest,
		ActionID: request.ActionID, ActionPhase: request.ActionPhase,
		AttemptID: request.AttemptID, RequestSHA256: digest, State: "succeeded",
	}
	for label, mutate := range map[string]func(*ActionReceipt){
		"other generation": func(r *ActionReceipt) { r.Generation = "00000000-0000-4000-8000-000000000004" },
		"other attempt":    func(r *ActionReceipt) { r.AttemptID = "00112233-4455-4677-8899-aabbccddeeee" },
		"other digest":     func(r *ActionReceipt) { r.RequestSHA256 = strings.Repeat("b", 64) },
		"failure claim":    func(r *ActionReceipt) { r.State = "failed" },
	} {
		t.Run(label, func(t *testing.T) {
			bad := receipt
			mutate(&bad)
			raw, err := json.Marshal(bad)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := DecodeActionReceipt(request, bytes.NewReader(raw)); err == nil {
				t.Fatal("false receipt accepted")
			}
		})
	}
}
