package guestproto

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
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
	result := SerialResult{Version: Version, StartGeneration: r.StartGeneration, Association: r.Association, CAFingerprint: r.CAFingerprint, Principal: r.Principal, HostPublicKey: testKey}
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

// This fails if an end frame can carry an association or trust result from a
// different bootstrap request.
func TestEncodeSerialFrameRejectsMismatchedResultCorrelation(t *testing.T) {
	r := testRequest()
	base := SerialResult{Version: Version, StartGeneration: r.StartGeneration, Association: r.Association, CAFingerprint: r.CAFingerprint, Principal: r.Principal, HostPublicKey: testKey}
	for name, mutate := range map[string]func(*SerialResult){
		"version":     func(result *SerialResult) { result.Version++ },
		"association": func(result *SerialResult) { result.BackendObject = "other" },
		"fingerprint": func(result *SerialResult) {
			result.CAFingerprint = "SHA256:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
		},
		"principal": func(result *SerialResult) {
			result.Principal = "boxwarden-session-00000000-0000-0000-0000-000000000000"
		},
	} {
		t.Run(name, func(t *testing.T) {
			result := base
			mutate(&result)
			if _, _, err := EncodeSerialFrame(r, result); err == nil {
				t.Fatal("mismatched result accepted")
			}
		})
	}
}

// This fails if frame parsing treats CR as JSON content instead of only the
// one PTY line ending, accepts loose JSON, or loses request correlation.
func TestSerialFrameIsUnambiguousWithPTYCRLF(t *testing.T) {
	r := testRequest()
	want := SerialResult{Version: Version, StartGeneration: r.StartGeneration, Association: r.Association, CAFingerprint: r.CAFingerprint, Principal: r.Principal, HostPublicKey: testKey}
	_, end, err := EncodeSerialFrame(r, want)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeSerialEndLine(r, end+"\r")
	if err != nil || got.Version != want.Version || got.StartGeneration != want.StartGeneration || got.Association != want.Association || got.CAFingerprint != want.CAFingerprint || got.Principal != want.Principal || got.HostPublicKey != want.HostPublicKey {
		t.Fatalf("DecodeSerialEndLine() = %#v, %v", got, err)
	}
	for _, invalid := range []string{end + "\r\r", end + "\n", strings.Replace(end, "BOXWARDEN-END", "BOXWARDEN-END extra", 1), strings.Replace(end, r.Nonce, "other", 1)} {
		if _, err := DecodeSerialEndLine(r, invalid); err == nil {
			t.Fatalf("invalid frame accepted: %q", invalid)
		}
	}
}

// This fails if a syntactically shaped authorized key contains arbitrary bytes
// rather than an exact RFC4253 ssh-ed25519 public-key blob.
func TestValidPublicKeyRequiresExactRFC4253Ed25519Blob(t *testing.T) {
	badPayload := base64.StdEncoding.EncodeToString(make([]byte, 51))
	wrongType := wireKey("ssh-rsa", make([]byte, 32), nil)
	wrongLength := wireKey("ssh-ed25519", make([]byte, 31), nil)
	trailing := wireKey("ssh-ed25519", make([]byte, 32), []byte{1})
	for name, key := range map[string]string{"arbitrary 51 bytes": "ssh-ed25519 " + badPayload, "wrong inner type": wrongType, "wrong key length": wrongLength, "trailing bytes": trailing} {
		t.Run(name, func(t *testing.T) {
			if validPublicKey(key) {
				t.Fatal("invalid wire blob accepted")
			}
		})
	}
}

func TestSerialRequestAndHostResultRejectMalformedEd25519Blob(t *testing.T) {
	bad := "ssh-ed25519 " + base64.StdEncoding.EncodeToString(make([]byte, 51))
	request := testRequest()
	request.CAPublicKey = bad
	request.CAFingerprint = testFingerprint(bad)
	if err := request.Validate(); err == nil {
		t.Fatal("malformed CA accepted")
	}
	b, _ := testBootstrapper(t)
	b.HostKeyPath = "/etc/ssh/bad.pub"
	if err := os.WriteFile(filepath.Join(b.Root, "etc/ssh/bad.pub"), []byte(bad+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Serial(context.Background(), testRequest()); err == nil {
		t.Fatal("malformed host key accepted")
	}
}

func wireKey(kind string, key, trailing []byte) string {
	var blob bytes.Buffer
	_ = binary.Write(&blob, binary.BigEndian, uint32(len(kind)))
	blob.WriteString(kind)
	_ = binary.Write(&blob, binary.BigEndian, uint32(len(key)))
	blob.Write(key)
	blob.Write(trailing)
	return "ssh-ed25519 " + base64.StdEncoding.EncodeToString(blob.Bytes())
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
	return strings.Join([]string{"trustedusercakeys /etc/ssh/boxwarden/active/trusted-user-ca.pub", "authorizedprincipalsfile /etc/ssh/boxwarden/active/authorized_principals/%u", "authorizedkeysfile none", "permituserenvironment no", "permituserrc no", "passwordauthentication no", "kbdinteractiveauthentication no", "permitrootlogin no", "x11forwarding no", "allowagentforwarding no", "allowtcpforwarding no", "allowstreamlocalforwarding no", "gatewayports no", "permittunnel no", ""}, "\n")
}

// This fails if removing or changing any golden-set effective sshd guard is
// admitted despite the host's `sshd -T` output being otherwise complete.
func TestVerifySSHDRejectsEachMissingOrChangedRequiredField(t *testing.T) {
	fields := []string{"trustedusercakeys", "authorizedprincipalsfile", "authorizedkeysfile", "permituserenvironment", "permituserrc", "passwordauthentication", "kbdinteractiveauthentication", "permitrootlogin", "x11forwarding", "allowagentforwarding", "allowtcpforwarding", "allowstreamlocalforwarding", "gatewayports", "permittunnel"}
	for _, field := range fields {
		for _, output := range []string{strings.Replace(sshdOutput(), field+" ", "", 1), strings.Replace(sshdOutput(), field+" ", field+" yes-", 1)} {
			t.Run(field, func(t *testing.T) {
				if _, err := NewBootstrapper(t.TempDir(), &fakeRunner{output: output}).verifySSHD(context.Background()); err == nil {
					t.Fatalf("admitted %s", field)
				}
			})
		}
	}
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
	bootstrapper := NewBootstrapper(root, &fakeRunner{output: sshdOutput()})
	bootstrapper.renameNoReplace = func(source, destination string) error { return os.Rename(source, destination) }
	return bootstrapper, parent
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

// This fails if an existing active tree can change expected entry type or mode
// and remain a trusted management/bootstrap binding.
func TestSerialBootstrapRejectsActiveTreeCorruption(t *testing.T) {
	for name, corrupt := range map[string]func(t *testing.T, parent string){
		"CA mode": func(t *testing.T, parent string) {
			if err := os.Chmod(filepath.Join(parent, "active/trusted-user-ca.pub"), 0o666); err != nil {
				t.Fatal(err)
			}
		},
		"manifest symlink": func(t *testing.T, parent string) {
			path := filepath.Join(parent, "active/management-binding.json")
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink("trusted-user-ca.pub", path); err != nil {
				t.Fatal(err)
			}
		},
		"principal type": func(t *testing.T, parent string) {
			path := filepath.Join(parent, "active/authorized_principals/boxwarden")
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(path, 0o755); err != nil {
				t.Fatal(err)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			b, parent := testBootstrapper(t)
			if _, err := b.Serial(context.Background(), testRequest()); err != nil {
				t.Fatal(err)
			}
			corrupt(t, parent)
			if _, err := b.Serial(context.Background(), testRequest()); err == nil {
				t.Fatal("corrupt active tree accepted")
			}
		})
	}
}

// This fails if an active target that appears after pre-publication validation
// can be replaced by the staged trust tree.
func TestSerialBootstrapRejectsTargetAppearingAtPublication(t *testing.T) {
	b, parent := testBootstrapper(t)
	b.renameNoReplace = func(_, destination string) error {
		if err := os.Mkdir(destination, 0o755); err != nil {
			return err
		}
		return fmt.Errorf("target appeared")
	}
	if _, err := b.Serial(context.Background(), testRequest()); err == nil {
		t.Fatal("publication race accepted")
	}
	info, err := os.Stat(filepath.Join(parent, "active"))
	if err != nil || !info.IsDir() {
		t.Fatalf("appeared target = %v, %v", info, err)
	}
}

func TestManagementAppliesAndReadsTypedZoneWithExactProgramBoundary(t *testing.T) {
	b, _ := testBootstrapper(t)
	if _, err := b.Serial(context.Background(), testRequest()); err != nil {
		t.Fatal(err)
	}
	zone := filepath.Join(b.Root, "etc/timezone")
	if err := os.WriteFile(zone, []byte("America/Chihuahua\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	read, err := b.Management(context.Background(), ManagementRequest{Version: Version, Kind: "read_zone", Association: testRequest().Association})
	if err != nil || string(read) != `{"version":1,"zone":"America/Chihuahua"}` {
		t.Fatalf("read_zone = %q, %v", read, err)
	}
	if _, err := b.Management(context.Background(), ManagementRequest{Version: Version, Kind: "apply_zone", Association: testRequest().Association, Zone: "America/Denver"}); err != nil {
		t.Fatal(err)
	}
	calls := b.Runner.(*fakeRunner).calls
	if len(calls) != 3 || strings.Join(calls[2], " ") != "/usr/bin/timedatectl set-timezone America/Denver" {
		t.Fatalf("calls = %#v", calls)
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
