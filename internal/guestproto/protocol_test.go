package guestproto

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const testKey = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
const testSession = "123e4567-e89b-42d3-a456-426614174000"
const testGeneration = "9b2d12d8-7014-4c5e-9d5c-627c2fcc1575"

// The serial helper must finish at LF even while its terminal stays open,
// and must leave following input untouched for its caller.
func TestSerialRequestConsumesOneCanonicalLineWithoutEOF(t *testing.T) {
	want := testRequest()
	encoded, _ := json.Marshal(want)
	r := &lineOnlyReader{data: append(encoded, '\n')}
	got, err := DecodeSerialRequest(r)
	if err != nil || got != want {
		t.Fatalf("decode before EOF = %#v, %v", got, err)
	}
	input := bytes.NewBuffer(append(append(encoded, '\n'), []byte("next line\n")...))
	if _, err := DecodeSerialRequest(input); err != nil || input.String() != "next line\n" {
		t.Fatalf("line boundary: remaining %q, error %v", input.String(), err)
	}
}

type lineOnlyReader struct{ data []byte }

func (r *lineOnlyReader) Read(p []byte) (int, error) {
	if len(r.data) == 0 {
		return 0, fmt.Errorf("read past canonical line without EOF")
	}
	n := copy(p, r.data)
	r.data = r.data[n:]
	return n, nil
}

func TestSerialRequestRejectsNoncanonicalOrUnboundedLine(t *testing.T) {
	encoded, _ := json.Marshal(testRequest())
	for _, input := range []string{string(encoded), string(encoded) + "\r\n", " " + string(encoded) + "\n", string(encoded) + " {}\n", strings.Repeat("x", MaxRequestBytes+1) + "\n", strings.Replace(string(encoded), `"version":1`, `"version": 1`, 1) + "\n"} {
		if _, err := DecodeSerialRequest(strings.NewReader(input)); err == nil {
			t.Fatal("accepted noncanonical request line")
		}
	}
	if _, err := DecodeManagementRequest(io.MultiReader(strings.NewReader(`{"version":1,"kind":"probe","domain":"work","session_id":"`+testSession+`","backend_kind":"tart","backend_object":"workstation"}`+"\n"), strings.NewReader("{}"))); err == nil {
		t.Fatal("management decoder lost EOF/trailing-data validation")
	}
}

func TestManagementIdentityInspectionAcceptsNoCallerParameters(t *testing.T) {
	request := ManagementRequest{Version: Version, Kind: "inspect_identity", Association: testRequest().Association}
	if err := request.Validate(); err != nil {
		t.Fatalf("fixed identity inspection rejected: %v", err)
	}
	request.Packages = []string{"git"}
	if err := request.Validate(); err == nil {
		t.Fatal("identity inspection accepted caller-selected package input")
	}
	request.Packages = nil
	request.Zone = "America/Denver"
	if err := request.Validate(); err == nil {
		t.Fatal("identity inspection accepted a time-zone mutation parameter")
	}
}

func TestManagementShutdownHasNoCallerParametersAndEnqueuesPoweroff(t *testing.T) {
	request := ManagementRequest{Version: Version, Kind: "request_shutdown", Association: testRequest().Association}
	if err := request.Validate(); err != nil {
		t.Fatalf("fixed shutdown rejected: %v", err)
	}
	for _, mutate := range []func(*ManagementRequest){
		func(r *ManagementRequest) { r.Zone = "America/Denver" },
		func(r *ManagementRequest) { r.Packages = []string{"git"} },
		func(r *ManagementRequest) {
			r.Workspaces = []WorkspaceMount{{VolumeID: "00112233-4455-4677-8899-aabbccddeeff", FilesystemUUID: "10213243-5465-4768-899a-bbccddeeff00", MountPath: "/home/boxwarden/workspaces/project"}}
		},
	} {
		changed := request
		mutate(&changed)
		if err := changed.Validate(); err == nil {
			t.Fatalf("shutdown accepted caller parameters: %+v", changed)
		}
	}
	b, _ := testBootstrapper(t)
	if _, err := b.Serial(context.Background(), testRequest()); err != nil {
		t.Fatal(err)
	}
	result, err := b.Management(context.Background(), request)
	if err != nil || string(result) != `{"version":1,"ok":true}` {
		t.Fatalf("shutdown result = %s, %v", result, err)
	}
	calls := b.Runner.(*fakeRunner).calls
	if got := calls[len(calls)-1]; !slices.Equal(got, []string{"/usr/bin/systemctl", "--no-block", "--no-wall", "--ignore-inhibitors", "poweroff"}) {
		t.Fatalf("shutdown argv = %#v", got)
	}
	runner := b.Runner.(*fakeRunner)
	before := len(runner.calls)
	foreign := request
	foreign.BackendObject = "foreign-system"
	if _, err := b.Management(context.Background(), foreign); err == nil || len(runner.calls) != before {
		t.Fatalf("foreign binding reached shutdown runner: %v", err)
	}
	runner.err = fmt.Errorf("systemd refused poweroff")
	if _, err := b.Management(context.Background(), request); err == nil {
		t.Fatal("failed poweroff was acknowledged")
	}
}

func TestInspectIdentityRejectsBuildResidueAndReportsFreshMachineIdentity(t *testing.T) {
	root := t.TempDir()
	for _, directory := range []string{"etc", "var/lib/boxwarden"} {
		if err := os.MkdirAll(filepath.Join(root, directory), 0755); err != nil {
			t.Fatal(err)
		}
	}
	write := func(name, value string, mode os.FileMode) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, name), []byte(value), mode); err != nil {
			t.Fatal(err)
		}
	}
	machineID := strings.Repeat("b", 32)
	write("etc/machine-id", machineID+"\n", 0644)
	write("etc/hostname", "boxwarden-"+machineID[:12]+"\n", 0644)
	write("etc/shadow", "root:!:1:0:99999:7:::\nboxwarden:!:1:0:99999:7:::\n", 0600)
	b := NewBootstrapper(root, nil)
	b.effectiveHostname = func() (string, error) { return "boxwarden-" + machineID[:12], nil }
	result, err := b.inspectIdentity()
	want := `{"version":1,"machine_id":"` + machineID + `","hostname":"boxwarden-` + machineID[:12] + `"}`
	if err != nil || string(result) != want {
		t.Fatalf("fresh identity = %s, %v; want %s", result, err, want)
	}
	b.effectiveHostname = func() (string, error) { return "boxwarden-task0-run-1", nil }
	if _, err := b.inspectIdentity(); err == nil {
		t.Fatal("stale effective kernel hostname accepted")
	}
	b.effectiveHostname = func() (string, error) { return "boxwarden-" + machineID[:12], nil }
	write("etc/boxwarden-task0-spike", "run-1\n", 0644)
	if _, err := b.inspectIdentity(); err == nil {
		t.Fatal("build marker survived identity inspection")
	}
	if err := os.Remove(filepath.Join(root, "etc/boxwarden-task0-spike")); err != nil {
		t.Fatal(err)
	}
	write("var/lib/boxwarden/golden-clone-ready", "", 0644)
	if _, err := b.inspectIdentity(); err == nil {
		t.Fatal("unconsumed clone-ready marker accepted")
	}
	if err := os.Remove(filepath.Join(root, "var/lib/boxwarden/golden-clone-ready")); err != nil {
		t.Fatal(err)
	}
	write("etc/shadow", "boxwarden:$6$recoverable:1:0:99999:7:::\n", 0600)
	if _, err := b.inspectIdentity(); err == nil {
		t.Fatal("recoverable builder verifier accepted")
	}
	write("etc/shadow", "boxwarden:!:1:0:99999:7:::\n", 0600)
	write("etc/hostname", "boxwarden-task0-run-1\n", 0644)
	if _, err := b.inspectIdentity(); err == nil {
		t.Fatal("build hostname accepted")
	}
	write("etc/hostname", "boxwarden-"+machineID[:12]+"\n", 0644)
	write("etc/shadow-", "boxwarden:$6$recoverable:1:0:99999:7:::\n", 0600)
	if _, err := b.inspectIdentity(); err == nil {
		t.Fatal("backup builder verifier accepted")
	}
}

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
			if _, err := DecodeSerialRequest(strings.NewReader(input + "\n")); err == nil {
				t.Fatal("invalid request accepted")
			}
		})
	}
}

// This fails if a response can be correlated to a different start attempt or
// loses either framing token used by the host serial parser.
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
	want := validSerialResult()
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

func validSerialResult() SerialResult {
	r := testRequest()
	return SerialResult{Version: Version, StartGeneration: r.StartGeneration, Association: r.Association, CAFingerprint: r.CAFingerprint, Principal: r.Principal, HostPublicKey: testKey, InstalledSHA256: map[string]string{"trusted-user-ca.pub": strings.Repeat("a", 64), "authorized_principals/boxwarden": strings.Repeat("b", 64), "management-binding.json": strings.Repeat("c", 64)}, SSHD: requiredSSHD}
}

// This fails if a frame can exceed the bounded wire envelope or if a base64
// payload is decoded before its derived decoded length is admitted.
func TestDecodeSerialEndLineRejectsEncodedAndDecodedOversizeBeforeDecode(t *testing.T) {
	r := testRequest()
	over := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte("x"), MaxResponseBytes+1))
	line := "BOXWARDEN-END " + r.Nonce + " " + r.SessionID + " " + over
	if _, err := DecodeSerialEndLine(r, line); err == nil {
		t.Fatal("oversized encoded line accepted")
	}
	boundary := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte("x"), MaxResponseBytes))
	line = "BOXWARDEN-END " + r.Nonce + " " + r.SessionID + " " + boundary
	if _, err := DecodeSerialEndLine(r, line); err == nil {
		t.Fatal("decoded-size boundary non-JSON payload accepted")
	}
	_, valid, err := EncodeSerialFrame(r, validSerialResult())
	if err != nil {
		t.Fatal(err)
	}
	raw, err := base64.StdEncoding.DecodeString(strings.Split(valid, " ")[3])
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) >= MaxResponseBytes {
		t.Fatal("fixture unexpectedly reaches boundary")
	}
	boundaryJSON := append(raw, bytes.Repeat([]byte(" "), MaxResponseBytes-len(raw))...)
	line = "BOXWARDEN-END " + r.Nonce + " " + r.SessionID + " " + base64.StdEncoding.EncodeToString(boundaryJSON)
	if _, err := DecodeSerialEndLine(r, line); err != nil {
		t.Fatalf("valid decoded-size boundary rejected: %v", err)
	}
}

// This fails if duplicate, unknown, missing, malformed, or changed nested
// result maps bypass the explicit active-tree and effective-sshd contract.
func TestDecodeSerialEndLineRequiresExactNestedMaps(t *testing.T) {
	r := testRequest()
	result := validSerialResult()
	_, valid, err := EncodeSerialFrame(r, result)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := DecodeSerialEndLine(r, valid); err != nil || len(got.InstalledSHA256) != 3 || len(got.SSHD) != len(requiredSSHD) {
		t.Fatalf("valid boundary result = %#v, %v", got, err)
	}
	for name, mutate := range map[string]func(string) string{
		"unknown digest": func(json string) string {
			return strings.Replace(json, `"installed_sha256":{`, `"installed_sha256":{"extra":"`+strings.Repeat("d", 64)+`",`, 1)
		},
		"missing digest": func(json string) string {
			return strings.Replace(json, `,"management-binding.json":"`+strings.Repeat("c", 64)+`"`, "", 1)
		},
		"duplicate digest": func(json string) string {
			return strings.Replace(json, `"trusted-user-ca.pub":"`+strings.Repeat("a", 64)+`"`, `"trusted-user-ca.pub":"`+strings.Repeat("a", 64)+`","trusted-user-ca.pub":"`+strings.Repeat("a", 64)+`"`, 1)
		},
		"malformed digest": func(json string) string {
			return strings.Replace(json, strings.Repeat("a", 64), strings.Repeat("A", 64), 1)
		},
		"unknown sshd": func(json string) string { return strings.Replace(json, `"sshd":{`, `"sshd":{"extra":"no",`, 1) },
		"missing sshd": func(json string) string { return strings.Replace(json, `,"gatewayports":"no"`, "", 1) },
		"duplicate sshd": func(json string) string {
			return strings.Replace(json, `"gatewayports":"no"`, `"gatewayports":"no","gatewayports":"no"`, 1)
		},
		"changed sshd": func(json string) string {
			return strings.Replace(json, `"gatewayports":"no"`, `"gatewayports":"yes"`, 1)
		},
	} {
		t.Run(name, func(t *testing.T) {
			raw, err := base64.StdEncoding.DecodeString(strings.Split(valid, " ")[3])
			if err != nil {
				t.Fatal(err)
			}
			line := "BOXWARDEN-END " + r.Nonce + " " + r.SessionID + " " + base64.StdEncoding.EncodeToString([]byte(mutate(string(raw))))
			if _, err := DecodeSerialEndLine(r, line); err == nil {
				t.Fatal("invalid nested map accepted")
			}
		})
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
	return strings.Join([]string{"pubkeyauthentication yes", "trustedusercakeys /etc/ssh/boxwarden/active/trusted-user-ca.pub", "authorizedprincipalsfile /etc/ssh/boxwarden/active/authorized_principals/%u", "authorizedkeysfile .ssh/authorized_keys", "permituserenvironment no", "permituserrc no", "passwordauthentication no", "kbdinteractiveauthentication no", "permitrootlogin no", "x11forwarding no", "allowagentforwarding no", "allowtcpforwarding no", "allowstreamlocalforwarding no", "gatewayports no", "permittunnel no", ""}, "\n")
}

func TestVerifySSHDAllowsWorkstationAuthorizedKeys(t *testing.T) {
	if _, err := NewBootstrapper(t.TempDir(), &fakeRunner{output: sshdOutput()}).verifySSHD(context.Background()); err != nil {
		t.Fatalf("workstation authorized_keys blocked management bootstrap: %v", err)
	}
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

// This fails if an unrelated `sshd -T` field becomes serial-frame state; the
// wire contract is only the complete guard set, not a host-config dump.
func TestVerifySSHDReturnsOnlyRequiredGuardSet(t *testing.T) {
	got, err := NewBootstrapper(t.TempDir(), &fakeRunner{output: sshdOutput() + "unusedsetting value\n"}).verifySSHD(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(requiredSSHD) {
		t.Fatalf("sshd result includes non-guard fields: %#v", got)
	}
	for key, want := range requiredSSHD {
		if got[key] != want {
			t.Fatalf("%s = %q, want %q", key, got[key], want)
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

// ssh-keygen -A writes a human comment after the ed25519 wire blob. The
// helper must pin the cryptographic key while returning canonical two-field
// material to the host's strict serial protocol.
func TestSerialBootstrapCanonicalizesGeneratedHostKeyComment(t *testing.T) {
	b, _ := testBootstrapper(t)
	path := filepath.Join(b.Root, "etc/ssh/ssh_host_ed25519_key.pub")
	if err := os.WriteFile(path, []byte(testKey+" root@boxwarden-123456789abc\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := b.Serial(context.Background(), testRequest())
	if err != nil {
		t.Fatal(err)
	}
	if result.HostPublicKey != testKey {
		t.Fatalf("host pin = %q, want canonical ed25519 key", result.HostPublicKey)
	}
}

func TestCanonicalGuestHostKeyRejectsMalformedTrailingMaterial(t *testing.T) {
	for name, input := range map[string]string{
		"second line":   testKey + "\nother-key",
		"two comments":  testKey + " root@host extra\n",
		"tab":           testKey + "\troot@host\n",
		"carriage":      testKey + " root@host\r\n",
		"empty comment": testKey + " \n",
		"bad key":       "ssh-ed25519 invalid root@host\n",
	} {
		t.Run(name, func(t *testing.T) {
			if got, err := canonicalGuestHostKey([]byte(input)); err == nil {
				t.Fatalf("admitted malformed host key %q", got)
			}
		})
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

func TestManagementInspectsExactInstalledPackagesWithoutShell(t *testing.T) {
	b, _ := testBootstrapper(t)
	if _, err := b.Serial(context.Background(), testRequest()); err != nil {
		t.Fatal(err)
	}
	runner := b.Runner.(*fakeRunner)
	runner.output = "install ok installed\t1:2.45.3-1ubuntu2\n"
	result, err := b.Management(context.Background(), ManagementRequest{Version: Version, Kind: "inspect_packages", Association: testRequest().Association, Packages: []string{"git"}})
	if err != nil {
		t.Fatal(err)
	}
	if string(result) != `{"version":1,"packages":[{"name":"git","version":"1:2.45.3-1ubuntu2"}]}` {
		t.Fatalf("package inspection = %q", result)
	}
	if got := runner.calls[len(runner.calls)-1]; !slices.Equal(got, []string{"/usr/bin/dpkg-query", "-W", "-f=${Status}\t${Version}", "--", "git"}) {
		t.Fatalf("dpkg argv = %#v", got)
	}
	for _, output := range []string{"deinstall ok config-files\t1.0\n", "install ok installed\t\n", "install ok installed\t1.0\nextra", "install ok installed\t" + strings.Repeat("a", 129)} {
		runner.output = output
		if _, err := b.Management(context.Background(), ManagementRequest{Version: Version, Kind: "inspect_packages", Association: testRequest().Association, Packages: []string{"git"}}); err == nil {
			t.Fatalf("accepted invalid dpkg result %q", output)
		}
	}
}

func TestManagementRejectsInvalidPackageInspectionRequests(t *testing.T) {
	base := ManagementRequest{Version: Version, Kind: "inspect_packages", Association: testRequest().Association}
	for _, packages := range [][]string{nil, {}, {"git", "git"}, {"git;id"}, {""}, {strings.Repeat("a", 129)}} {
		request := base
		request.Packages = packages
		if err := request.Validate(); err == nil {
			t.Fatalf("accepted packages %#v", packages)
		}
	}
	base.Packages = []string{"git"}
	base.Zone = "UTC"
	if err := base.Validate(); err == nil {
		t.Fatal("accepted zone on package inspection")
	}
	base.Kind = "probe"
	base.Zone = ""
	if err := base.Validate(); err == nil {
		t.Fatal("accepted package list on probe")
	}
	valid := `{"version":1,"kind":"inspect_packages","domain":"work","session_id":"` + testSession + `","backend_kind":"tart","backend_object":"workstation","packages":["git"]}`
	decoded, err := DecodeManagementRequest(strings.NewReader(valid))
	if err != nil || !slices.Equal(decoded.Packages, []string{"git"}) {
		t.Fatalf("typed package request = %#v, %v", decoded, err)
	}
	for _, input := range []string{
		strings.Replace(valid, `"packages":["git"]`, `"packages":["git"],"packages":["git"]`, 1),
		strings.Replace(valid, `"packages":["git"]`, `"packages":["git"],"command":"id"`, 1),
		strings.Replace(valid, `"kind":"inspect_packages"`, `"kind":"probe"`, 1),
		strings.Replace(strings.Replace(valid, `"kind":"inspect_packages"`, `"kind":"probe"`, 1), `"packages":["git"]`, `"packages":[]`, 1),
	} {
		if _, err := DecodeManagementRequest(strings.NewReader(input)); err == nil {
			t.Fatalf("accepted malformed package request %q", input)
		}
	}
}

func TestManagementDecodesMinimalProbeWithoutOptionalFields(t *testing.T) {
	input := `{"version":1,"kind":"probe","domain":"work","session_id":"` + testSession + `","backend_kind":"tart","backend_object":"workstation"}`
	decoded, err := DecodeManagementRequest(strings.NewReader(input))
	if err != nil || decoded.Kind != "probe" || decoded.Zone != "" || decoded.Packages != nil {
		t.Fatalf("minimal management probe rejected: %+v, %v", decoded, err)
	}
}

func TestManagementWorkspaceRequestRequiresExactBoundedMounts(t *testing.T) {
	first := WorkspaceMount{VolumeID: "00112233-4455-4677-8899-aabbccddeeff", FilesystemUUID: "10213243-5465-4768-899a-bbccddeeff00", MountPath: "/home/boxwarden/workspaces/project"}
	base := ManagementRequest{Version: Version, Kind: "ensure_workspaces", Association: testRequest().Association, Workspaces: []WorkspaceMount{first}}
	if err := base.Validate(); err != nil {
		t.Fatalf("exact workspace request rejected: %v", err)
	}
	for _, tc := range []struct {
		name string
		edit func(*ManagementRequest)
	}{
		{"missing", func(r *ManagementRequest) { r.Workspaces = nil }},
		{"duplicate-volume", func(r *ManagementRequest) { r.Workspaces = append(r.Workspaces, first) }},
		{"too-many", func(r *ManagementRequest) { r.Workspaces = []WorkspaceMount{first, first, first, first, first} }},
		{"duplicate-uuid", func(r *ManagementRequest) {
			next := first
			next.VolumeID = "20112233-4455-4677-8899-aabbccddeeff"
			next.MountPath += "2"
			r.Workspaces = append(r.Workspaces, next)
		}},
		{"duplicate-path", func(r *ManagementRequest) {
			next := first
			next.VolumeID = "20112233-4455-4677-8899-aabbccddeeff"
			next.FilesystemUUID = "20213243-5465-4768-899a-bbccddeeff00"
			r.Workspaces = append(r.Workspaces, next)
		}},
		{"wrong-path", func(r *ManagementRequest) { r.Workspaces[0].MountPath = "/tmp/project" }},
		{"wrong-uuid", func(r *ManagementRequest) { r.Workspaces[0].FilesystemUUID = "bad" }},
		{"with-zone", func(r *ManagementRequest) { r.Zone = "UTC" }},
		{"with-packages", func(r *ManagementRequest) { r.Packages = []string{"git"} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := base
			request.Workspaces = append([]WorkspaceMount(nil), base.Workspaces...)
			tc.edit(&request)
			if err := request.Validate(); err == nil {
				t.Fatalf("invalid workspace request accepted: %+v", request)
			}
		})
	}
	base.Kind = "probe"
	if err := base.Validate(); err != nil {
		t.Fatalf("mount-bound probe rejected: %v", err)
	}
	valid := `{"version":1,"kind":"ensure_workspaces","domain":"work","session_id":"` + testSession + `","backend_kind":"tart","backend_object":"workstation","workspaces":[{"volume_id":"00112233-4455-4677-8899-aabbccddeeff","filesystem_uuid":"10213243-5465-4768-899a-bbccddeeff00","mount_path":"/home/boxwarden/workspaces/project"}]}`
	decoded, err := DecodeManagementRequest(strings.NewReader(valid))
	if err != nil || decoded.Kind != "ensure_workspaces" || !slices.Equal(decoded.Workspaces, []WorkspaceMount{first}) {
		t.Fatalf("typed workspace request = %+v, %v", decoded, err)
	}
	for _, bad := range []string{
		strings.Replace(valid, `"workspaces":[`, `"zone":"","workspaces":[`, 1),
		strings.Replace(valid, `"workspaces":[`, `"zone":null,"workspaces":[`, 1),
		strings.Replace(valid, `"mount_path":"/home/boxwarden/workspaces/project"`, `"mount_path":"/home/boxwarden/workspaces/project","mount_path":"/home/boxwarden/workspaces/project"`, 1),
		strings.Replace(valid, `"mount_path":"/home/boxwarden/workspaces/project"`, `"mount_path":"/tmp/project"`, 1),
		strings.Replace(valid, `"workspaces":[`, `"workspaces":[],"workspaces":[`, 1),
		strings.Replace(valid, `"mount_path":"/home/boxwarden/workspaces/project"`, `"mount_path":"/home/boxwarden/workspaces/project","extra":true`, 1),
	} {
		if _, err := DecodeManagementRequest(strings.NewReader(bad)); err == nil {
			t.Fatalf("malformed workspace request accepted: %s", bad)
		}
	}
}

type workspaceRunner struct {
	calls     [][]string
	mounted   bool
	source    string
	uuid      string
	options   string
	fsOptions string
}

func (r *workspaceRunner) Run(_ context.Context, path string, args ...string) ([]byte, error) {
	r.calls = append(r.calls, append([]string{path}, args...))
	switch path {
	case "/usr/sbin/blkid":
		return []byte("/dev/vdb\n"), nil
	case "/usr/bin/findmnt":
		if !r.mounted {
			return nil, fmt.Errorf("no mount")
		}
		options := r.options
		if options == "" {
			options = "rw,relatime"
		}
		fsOptions := r.fsOptions
		if fsOptions == "" {
			fsOptions = "rw,errors=remount-ro"
		}
		return []byte(fmt.Sprintf("%s ext4 %s %s %s\n", r.source, r.uuid, options, fsOptions)), nil
	case "/usr/bin/mount":
		r.mounted = true
		return nil, nil
	default:
		return nil, fmt.Errorf("unexpected command %q", path)
	}
}

func TestManagementEnsuresAndProbesExactWorkspaceWithoutShell(t *testing.T) {
	b, _ := testBootstrapper(t)
	if err := os.MkdirAll(filepath.Join(b.Root, "home/boxwarden"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(b.Root, "proc/self"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(b.Root, "proc/self/mountinfo"), []byte("1 0 0:1 / / rw - ext4 /dev/vda rw\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Serial(context.Background(), testRequest()); err != nil {
		t.Fatal(err)
	}
	owner := os.Getuid()
	group := os.Getgid()
	b.workspaceOwner = func() (int, int, error) { return owner, group, nil }
	mount := WorkspaceMount{VolumeID: "00112233-4455-4677-8899-aabbccddeeff", FilesystemUUID: "10213243-5465-4768-899a-bbccddeeff00", MountPath: "/home/boxwarden/workspaces/project"}
	runner := &workspaceRunner{source: "/dev/vdb", uuid: mount.FilesystemUUID}
	b.Runner = runner
	request := ManagementRequest{Version: Version, Kind: "ensure_workspaces", Association: testRequest().Association, Workspaces: []WorkspaceMount{mount}}
	for i := 0; i < 2; i++ {
		result, err := b.Management(context.Background(), request)
		if err != nil || string(result) != `{"version":1,"ok":true}` {
			t.Fatalf("ensure attempt %d = %s, %v", i, result, err)
		}
	}
	request.Kind = "probe"
	result, err := b.Management(context.Background(), request)
	if err != nil || string(result) != `{"version":1,"ok":true}` {
		t.Fatalf("mount-bound probe = %s, %v", result, err)
	}
	want := [][]string{
		{"/usr/sbin/blkid", "-t", "UUID=" + mount.FilesystemUUID, "-o", "device"},
		{"/usr/bin/findmnt", "-n", "-o", "SOURCE,FSTYPE,UUID,VFS-OPTIONS,FS-OPTIONS", "--mountpoint", mount.MountPath},
		{"/usr/bin/mount", "-t", "ext4", "-o", "nodev,nosuid", "/dev/vdb", mount.MountPath},
		{"/usr/bin/findmnt", "-n", "-o", "SOURCE,FSTYPE,UUID,VFS-OPTIONS,FS-OPTIONS", "--mountpoint", mount.MountPath},
	}
	if len(runner.calls) < len(want) || !slices.EqualFunc(runner.calls[:len(want)], want, slices.Equal[[]string]) {
		t.Fatalf("first mount argv = %#v", runner.calls)
	}
	mounts := 0
	for _, call := range runner.calls {
		if call[0] == "/usr/bin/mount" {
			mounts++
		}
	}
	if mounts != 1 {
		t.Fatalf("idempotent ensure repeated mount: %#v", runner.calls)
	}
	runner.source = "/dev/vdc"
	if _, err := b.Management(context.Background(), request); err == nil {
		t.Fatal("wrong mounted device passed probe")
	}
	request.Kind = "ensure_workspaces"
	if _, err := b.Management(context.Background(), request); err == nil {
		t.Fatal("wrong existing mount accepted or replaced")
	}
	if got := len(runner.calls); got == 0 || runner.calls[got-1][0] == "/usr/bin/mount" {
		t.Fatal("wrong existing mount was replaced")
	}
	request.Kind = "probe"
	runner.source = "/dev/vdb"
	runner.uuid = "20213243-5465-4768-899a-bbccddeeff00"
	if _, err := b.Management(context.Background(), request); err == nil {
		t.Fatal("wrong mounted filesystem UUID passed probe")
	}
	runner.uuid = mount.FilesystemUUID
	runner.options = "ro,relatime"
	if err := os.WriteFile(filepath.Join(b.Root, "proc/self/mountinfo"), []byte("1 0 0:1 / "+mount.MountPath+" rw - ext4 /dev/vdb rw\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Management(context.Background(), request); err == nil {
		t.Fatal("read-only workspace passed readiness probe")
	}
	request.Kind = "ensure_workspaces"
	if _, err := b.Management(context.Background(), request); err == nil {
		t.Fatal("read-only existing workspace passed ensure")
	}
	runner.options = "rw,relatime"
	runner.fsOptions = "ro,errors=remount-ro"
	request.Kind = "probe"
	if _, err := b.Management(context.Background(), request); err == nil {
		t.Fatal("read-only ext4 superblock passed readiness probe")
	}
	request.Kind = "ensure_workspaces"
	if _, err := b.Management(context.Background(), request); err == nil {
		t.Fatal("read-only ext4 superblock passed ensure")
	}
}

func TestWorkspaceMountRejectsUnsafePathComponentsAndAmbiguousDevices(t *testing.T) {
	b := NewBootstrapper(t.TempDir(), &workspaceRunner{})
	if err := os.MkdirAll(filepath.Join(b.Root, "home/boxwarden"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(b.Root, "home/boxwarden/workspaces")); err != nil {
		t.Fatal(err)
	}
	if err := b.checkWorkspaceDirectory("/home/boxwarden/workspaces/project", true); err == nil {
		t.Fatal("symlinked mount parent accepted")
	}
	for _, device := range []string{"/dev/vdb\n/dev/vdc", "/tmp/disk", "/dev/../vdb", "/dev/vdb extra"} {
		if validWorkspaceDevice(device) {
			t.Fatalf("unsafe device path accepted: %q", device)
		}
	}
}

func TestWorkspaceProbeRejectsSymlinkedAncestorOnExistingMount(t *testing.T) {
	b := NewBootstrapper(t.TempDir(), &workspaceRunner{})
	parent := filepath.Join(b.Root, "home/boxwarden/workspaces")
	if err := os.MkdirAll(filepath.Join(parent, "project"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(parent, parent+"-moved"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(parent+"-moved", parent); err != nil {
		t.Fatal(err)
	}
	if err := b.checkWorkspaceDirectory("/home/boxwarden/workspaces/project", false); err == nil {
		t.Fatal("existing mount probe accepted a symlinked parent")
	}
}

func TestWorkspaceMountDoesNotOvermountAfterFailedInspection(t *testing.T) {
	b := NewBootstrapper(t.TempDir(), &workspaceRunner{})
	if err := os.MkdirAll(filepath.Join(b.Root, "proc/self"), 0o755); err != nil {
		t.Fatal(err)
	}
	path := "/home/boxwarden/workspaces/project"
	line := "1 0 0:1 / " + path + " rw - ext4 /dev/vdc rw\n"
	if err := os.WriteFile(filepath.Join(b.Root, "proc/self/mountinfo"), []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := b.workspaceMountAbsent(path); err == nil {
		t.Fatal("unverified existing mount could be overmounted")
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
// than accepting only the declared typed request kinds.
func TestManagementRejectsRemoteCommandSurface(t *testing.T) {
	for _, input := range []string{`{"version":1,"kind":"exec","domain":"work","session_id":"123e4567-e89b-42d3-a456-426614174000","backend_kind":"tart","backend_object":"workstation","command":"id"}`, `{"version":1,"kind":"probe","domain":"work","session_id":"123e4567-e89b-42d3-a456-426614174000","backend_kind":"tart","backend_object":"workstation","zone":"UTC"}`} {
		if _, err := DecodeManagementRequest(strings.NewReader(input)); err == nil {
			t.Fatal("remote command surface accepted")
		}
	}
}
