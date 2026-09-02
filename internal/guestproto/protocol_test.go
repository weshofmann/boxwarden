package guestproto

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"strings"
	"testing"
)

const testKey = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
const testSession = "123e4567-e89b-42d3-a456-426614174000"

func testSerialRequest() SerialRequest {
	return SerialRequest{Version: Version, Nonce: "nonce-1", StartGeneration: 7, Association: Association{Domain: "work", SessionID: testSession, BackendKind: "tart", BackendObject: "workstation"}, CAPublicKey: testKey, CAFingerprint: testFingerprint(testKey), Principal: "boxwarden-session-" + testSession}
}
func testFingerprint(key string) string {
	parts := strings.Fields(key)
	decoded, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil {
		panic(err)
	}
	sum := sha256.Sum256(decoded)
	return "SHA256:" + base64.RawStdEncoding.EncodeToString(sum[:])
}

func TestDecodeSerialRequestRejectsDuplicateAndOversizedJSON(t *testing.T) {
	request := testSerialRequest()
	encoded := fmt.Sprintf(`{"version":1,"nonce":%q,"start_generation":7,"domain":"work","session_id":%q,"backend_kind":"tart","backend_object":"workstation","ca_public_key":%q,"ca_fingerprint":%q,"principal":%q}`, request.Nonce, request.SessionID, request.CAPublicKey, request.CAFingerprint, request.Principal)
	if _, err := DecodeSerialRequest(strings.NewReader(encoded)); err != nil {
		t.Fatalf("DecodeSerialRequest(valid): %v", err)
	}
	if _, err := DecodeSerialRequest(strings.NewReader(strings.Replace(encoded, `"version":1`, `"version":1,"version":1`, 1))); err == nil {
		t.Fatal("duplicate version accepted")
	}
	if _, err := DecodeSerialRequest(bytes.NewReader(bytes.Repeat([]byte("x"), MaxRequestBytes+1))); err == nil {
		t.Fatal("oversized request accepted")
	}
}

func TestSerialRequestRequiresExactDerivedPrincipalAndFingerprint(t *testing.T) {
	request := testSerialRequest()
	request.Principal = "boxwarden-session-other"
	if err := request.Validate(); err == nil {
		t.Fatal("mismatched principal accepted")
	}
	request = testSerialRequest()
	request.CAFingerprint = "SHA256:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	if err := request.Validate(); err == nil {
		t.Fatal("mismatched CA fingerprint accepted")
	}
}

func TestSerialFrameEchoesCorrelationWithoutInstallingIt(t *testing.T) {
	request := testSerialRequest()
	result := SerialResult{Version: Version, Association: request.Association, CAFingerprint: request.CAFingerprint, Principal: request.Principal}
	begin, end, err := EncodeSerialFrame(request, result)
	if err != nil {
		t.Fatal(err)
	}
	if begin != "BOXWARDEN-BEGIN nonce-1 "+testSession || !strings.HasPrefix(end, "BOXWARDEN-END nonce-1 "+testSession+" ") {
		t.Fatalf("frame = %q / %q", begin, end)
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.Split(end, " ")[3])
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(decoded, []byte("nonce")) || bytes.Contains(decoded, []byte("generation")) {
		t.Fatalf("correlation leaked into result: %s", decoded)
	}
}
