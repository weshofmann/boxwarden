package guestproto

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testKey = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
const testSession = "123e4567-e89b-42d3-a456-426614174000"
const testGeneration = "9b2d12d8-7014-4c5e-9d5c-627c2fcc1575"

func testRequest() SerialRequest {
	return SerialRequest{Version: Version, Nonce: "nonce-1", StartGeneration: testGeneration, Association: Association{Domain: "work", SessionID: testSession, BackendKind: "tart", BackendObject: "workstation"}, CAPublicKey: testKey, CAFingerprint: testFingerprint(testKey), Principal: "boxwarden-session-" + testSession}
}

func testFingerprint(key string) string {
	parts := strings.Fields(key)
	raw, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil {
		panic(err)
	}
	sum := sha256.Sum256(raw)
	return "SHA256:" + base64.RawStdEncoding.EncodeToString(sum[:])
}

// This fails if permissive JSON decoding accepts a field that could widen the
// privileged guest protocol, or if correlation/binding fields are mismatched.
func TestSerialRequestRejectsUnknownOrMismatchedFields(t *testing.T) {
	r := testRequest()
	valid := fmt.Sprintf(`{"version":1,"nonce":%q,"start_generation":%q,"domain":"work","session_id":%q,"backend_kind":"tart","backend_object":"workstation","ca_public_key":%q,"ca_fingerprint":%q,"principal":%q}`, r.Nonce, r.StartGeneration, r.SessionID, r.CAPublicKey, r.CAFingerprint, r.Principal)
	for name, input := range map[string]string{
		"unknown":            strings.Replace(valid, "}", `,"command":"id"}`, 1),
		"numeric generation": strings.Replace(valid, `"start_generation":"`+testGeneration+`"`, `"start_generation":7`, 1),
		"wrong principal":    strings.Replace(valid, r.Principal, "boxwarden-session-00000000-0000-0000-0000-000000000000", 1),
		"wrong fingerprint":  strings.Replace(valid, r.CAFingerprint, "SHA256:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", 1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := DecodeSerialRequest(strings.NewReader(input)); err == nil {
				t.Fatal("invalid request accepted")
			}
		})
	}
}

// This fails if a response can be correlated to a different start attempt or
// loses either framing token used by the host broker.
func TestEncodeSerialFrameRoundTripsExactGenerationAndNonce(t *testing.T) {
	r := testRequest()
	result := SerialResult{Version: Version, StartGeneration: r.StartGeneration, Association: r.Association, CAFingerprint: r.CAFingerprint, Principal: r.Principal}
	begin, end, err := EncodeSerialFrame(r, result)
	if err != nil {
		t.Fatal(err)
	}
	if begin != "BOXWARDEN-BEGIN nonce-1 "+testSession {
		t.Fatalf("begin = %q", begin)
	}
	fields := strings.Split(end, " ")
	if len(fields) != 4 || fields[0] != "BOXWARDEN-END" || fields[1] != r.Nonce || fields[2] != r.SessionID {
		t.Fatalf("end = %q", end)
	}
	decoded, err := base64.StdEncoding.DecodeString(fields[3])
	if err != nil {
		t.Fatal(err)
	}
	var got SerialResult
	if err := json.Unmarshal(decoded, &got); err != nil {
		t.Fatal(err)
	}
	if got.StartGeneration != testGeneration || got.Association != r.Association {
		t.Fatalf("decoded result = %#v", got)
	}
	result.StartGeneration = "80c64529-fcb5-4789-8460-a43517622238"
	if _, _, err := EncodeSerialFrame(r, result); err == nil {
		t.Fatal("different generation accepted")
	}
}

// This fails if frame parsing treats CR as JSON content instead of only a PTY
// line ending, which would make an otherwise canonical base64 payload ambiguous.
func TestSerialFrameIsUnambiguousWithPTYCRLF(t *testing.T) {
	r := testRequest()
	_, end, err := EncodeSerialFrame(r, SerialResult{Version: Version, StartGeneration: r.StartGeneration, Association: r.Association, CAFingerprint: r.CAFingerprint, Principal: r.Principal})
	if err != nil {
		t.Fatal(err)
	}
	line := strings.TrimSuffix(end+"\r\n", "\n")
	line = strings.TrimSuffix(line, "\r")
	payload := strings.Split(line, " ")[3]
	decoded, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(decoded, []byte{'\r', '\n'}) {
		t.Fatalf("unexpected line ending in decoded JSON: %q", decoded)
	}
}

type fakeRunner struct {
	calls  [][]string
	output string
	err    error
}

func (r *fakeRunner) Run(_ context.Context, path string, args ...string) ([]byte, error) {
	r.calls = append(r.calls, append([]string{path}, args...))
	if r.err != nil {
		return nil, r.err
	}
	if len(args) > 0 && args[0] == "-t" {
		return nil, nil
	}
	return []byte(r.output), nil
}
func sshdOutput() string {
	return strings.Join([]string{"trustedusercakeys /etc/ssh/boxwarden/active/trusted-user-ca.pub", "authorizedprincipalsfile /etc/ssh/boxwarden/active/authorized_principals/%u", "authorizedkeysfile none", "permituserenvironment no", "permituserrc no", "passwordauthentication no", "kbdinteractiveauthentication no", "permitrootlogin no", "x11forwarding no", "allowtcpforwarding no", "allowstreamlocalforwarding no", "permittunnel no", ""}, "\n")
}
func testBootstrapper(t *testing.T) (*Bootstrapper, string) {
	t.Helper()
	root := t.TempDir()
	parent := filepath.Join(root, "etc/ssh/boxwarden")
	if err := os.MkdirAll(parent, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(parent, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "etc/ssh/ssh_host_ed25519_key.pub"), []byte(testKey+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return NewBootstrapper(root, &fakeRunner{output: sshdOutput()}), parent
}

type shortWriter struct{}

func (shortWriter) Write(data []byte) (int, error) { return len(data) - 1, nil }

// This fails if a partial privileged-state write is treated as a durable
// publication merely because the operating system did not return an error.
func TestWriteExactRejectsShortWrite(t *testing.T) {
	if err := writeExact(shortWriter{}, []byte("binding")); err == nil {
		t.Fatal("short write accepted")
	}
}

// This fails if bootstrap leaks association material outside the single atomic
// active tree, or stores replay-only nonce/generation values durably.
func TestSerialBootstrapPublishesOnlyDurableBinding(t *testing.T) {
	b, parent := testBootstrapper(t)
	if _, err := b.Serial(context.Background(), testRequest()); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(parent)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "active" {
		t.Fatalf("parent entries = %#v", entries)
	}
	manifest, err := os.ReadFile(filepath.Join(parent, "active/management-binding.json"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(manifest, []byte("nonce")) || bytes.Contains(manifest, []byte("generation")) {
		t.Fatalf("transient correlation persisted: %s", manifest)
	}
}

// This fails if a compromised or incomplete active directory can add material
// that bypasses the fixed binding layout.
func TestSerialBootstrapRejectsUnexpectedActiveEntries(t *testing.T) {
	b, parent := testBootstrapper(t)
	if _, err := b.Serial(context.Background(), testRequest()); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(parent, "active/unexpected"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Serial(context.Background(), testRequest()); err == nil {
		t.Fatal("active tree with unexpected entry accepted")
	}
}

// This fails if retry changes a durable trust binding instead of returning the
// existing exact association, CA fingerprint, and derived principal.
func TestSerialBootstrapIsIdempotentAndRejectsConflictingBinding(t *testing.T) {
	b, _ := testBootstrapper(t)
	r := testRequest()
	if _, err := b.Serial(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	r.StartGeneration = "80c64529-fcb5-4789-8460-a43517622238"
	if _, err := b.Serial(context.Background(), r); err != nil {
		t.Fatalf("idempotent retry: %v", err)
	}
	r.CAPublicKey = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAB"
	r.CAFingerprint = testFingerprint(r.CAPublicKey)
	if _, err := b.Serial(context.Background(), r); err == nil {
		t.Fatal("conflicting binding accepted")
	}
}

// This fails if management acquires a shell or untyped command surface rather
// than accepting only the three declared request kinds.
func TestManagementRejectsRemoteCommandSurface(t *testing.T) {
	for _, input := range []string{`{"version":1,"kind":"exec","domain":"work","session_id":"123e4567-e89b-42d3-a456-426614174000","backend_kind":"tart","backend_object":"workstation","command":"id"}`, `{"version":1,"kind":"probe","domain":"work","session_id":"123e4567-e89b-42d3-a456-426614174000","backend_kind":"tart","backend_object":"workstation","zone":"UTC"}`} {
		if _, err := DecodeManagementRequest(strings.NewReader(input)); err == nil {
			t.Fatal("remote command surface accepted")
		}
	}
}
