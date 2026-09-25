package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/weshofmann/boxwarden/internal/guestproto"
)

type errorWriter struct{}

type mainActionExecutor struct{ calls int }

func (r *mainActionExecutor) Run(_ context.Context, argv []string) error {
	r.calls++
	if len(argv) != 1 || argv[0] != "/usr/bin/true" {
		return errors.New("action argv changed")
	}
	return nil
}

func TestMainActionModeReturnsExactReceiptWithoutReplay(t *testing.T) {
	b, _ := managementFixture(t)
	base := filepath.Join(b.Root, "var/lib/boxwarden")
	if err := os.MkdirAll(base, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(base, 0o700); err != nil {
		t.Fatal(err)
	}
	executor := &mainActionExecutor{}
	b.ActionExecutor = executor
	request := guestproto.ActionRequest{
		Version:      guestproto.Version,
		Association:  guestproto.Association{Domain: "work", SessionID: "123e4567-e89b-42d3-a456-426614174000", BackendKind: "tart", BackendObject: "workstation"},
		Generation:   "9b2d12d8-7014-4c5e-9d5c-627c2fcc1575",
		RecipeDigest: strings.Repeat("a", 64), ActionID: "setup", ActionPhase: "once",
		AttemptID: "00112233-4455-4677-8899-aabbccddeeff", Argv: []string{"/usr/bin/true"},
	}
	input, _, err := guestproto.EncodeActionRequest(request)
	if err != nil {
		t.Fatal(err)
	}
	var first bytes.Buffer
	if err := run([]string{"action"}, bytes.NewReader(input), &first, &bytes.Buffer{}, b); err != nil {
		t.Fatal(err)
	}
	if _, err := guestproto.DecodeActionReceipt(request, bytes.NewReader(first.Bytes())); err != nil {
		t.Fatalf("helper returned invalid receipt: %v", err)
	}
	var second bytes.Buffer
	if err := run([]string{"action"}, bytes.NewReader(input), &second, &bytes.Buffer{}, b); err != nil || second.String() != first.String() || executor.calls != 1 {
		t.Fatalf("exact retry = %v, %q, calls=%d", err, second.String(), executor.calls)
	}
}

func (errorWriter) Write([]byte) (int, error) { return 0, errors.New("write failed") }

type shortOutputWriter struct{}

func (shortOutputWriter) Write(data []byte) (int, error) { return len(data) - 1, nil }

type secondWriteError struct{ writes int }

func (w *secondWriteError) Write(data []byte) (int, error) {
	w.writes++
	if w.writes == 2 {
		return 0, errors.New("second serial frame write failed")
	}
	return len(data), nil
}

// This fails if helper success is reported after the serial response could not
// reach the host serial parser.
func TestMainPropagatesOutputFailure(t *testing.T) {
	bootstrapper, input := managementFixture(t)
	if err := run([]string{"management"}, input, errorWriter{}, &bytes.Buffer{}, bootstrapper); err == nil {
		t.Fatal("output failure accepted")
	}
}

// This fails if a short-but-errorless serial transport write is reported as a
// successful management reply.
func TestMainPropagatesShortOutputFailure(t *testing.T) {
	bootstrapper, input := managementFixture(t)
	if err := run([]string{"management"}, input, shortOutputWriter{}, &bytes.Buffer{}, bootstrapper); err == nil {
		t.Fatal("short output accepted")
	}
}

// This fails if a complete begin frame makes a failed end-frame write appear
// successful to the serial parser.
func TestMainPropagatesSecondSerialFrameWriteFailure(t *testing.T) {
	bootstrapper, input := serialFixture(t)
	writer := &secondWriteError{}
	err := run([]string{"serial-bootstrap"}, input, writer, &bytes.Buffer{}, bootstrapper)
	if err == nil {
		t.Fatal("second serial frame failure accepted")
	}
	t.Logf("serial run failure = %v", err)
	if writer.writes != 2 {
		t.Fatalf("writes = %d, want 2", writer.writes)
	}
}

func managementFixture(t *testing.T) (*guestproto.Bootstrapper, *bytes.Buffer) {
	t.Helper()
	root := t.TempDir()
	active := filepath.Join(root, "etc/ssh/boxwarden/active/authorized_principals")
	if err := os.MkdirAll(active, 0o755); err != nil {
		t.Fatal(err)
	}
	key := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	raw, err := base64.StdEncoding.DecodeString(strings.Fields(key)[1])
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	fingerprint := "SHA256:" + base64.RawStdEncoding.EncodeToString(sum[:])
	if err := os.WriteFile(filepath.Join(filepath.Dir(active), "management-binding.json"), []byte(`{"version":1,"domain":"work","session_id":"123e4567-e89b-42d3-a456-426614174000","backend_kind":"tart","backend_object":"workstation","ca_fingerprint":"`+fingerprint+`","principal":"boxwarden-session-123e4567-e89b-42d3-a456-426614174000"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(active), "trusted-user-ca.pub"), []byte(key+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(active, "boxwarden"), []byte("boxwarden-session-123e4567-e89b-42d3-a456-426614174000\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var input bytes.Buffer
	input.WriteString(`{"version":1,"kind":"probe","domain":"work","session_id":"123e4567-e89b-42d3-a456-426614174000","backend_kind":"tart","backend_object":"workstation"}`)
	return guestproto.NewBootstrapper(root, nil), &input
}

type helperRunner struct{}

func (helperRunner) Run(_ context.Context, path string, args ...string) ([]byte, error) {
	if path != "/usr/sbin/sshd" {
		return nil, errors.New("unexpected program")
	}
	if len(args) == 1 && args[0] == "-t" {
		return nil, nil
	}
	if len(args) == 3 && args[0] == "-T" {
		return []byte("trustedusercakeys /etc/ssh/boxwarden/active/trusted-user-ca.pub\nauthorizedprincipalsfile /etc/ssh/boxwarden/active/authorized_principals/%u\nauthorizedkeysfile .ssh/authorized_keys\npermituserenvironment no\npermituserrc no\npasswordauthentication no\nkbdinteractiveauthentication no\npermitrootlogin no\nx11forwarding no\nallowagentforwarding no\nallowtcpforwarding no\nallowstreamlocalforwarding no\ngatewayports no\npermittunnel no\n"), nil
	}
	return nil, errors.New("unexpected sshd arguments")
}

func serialFixture(t *testing.T) (*guestproto.Bootstrapper, *bytes.Buffer) {
	t.Helper()
	bootstrapper, _ := managementFixture(t)
	bootstrapper.Runner = helperRunner{}
	key := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	if err := os.WriteFile(filepath.Join(bootstrapper.Root, "etc/ssh/ssh_host_ed25519_key.pub"), []byte(key+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	raw, err := base64.StdEncoding.DecodeString(strings.Fields(key)[1])
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	fingerprint := "SHA256:" + base64.RawStdEncoding.EncodeToString(sum[:])
	var input bytes.Buffer
	input.WriteString(`{"version":1,"nonce":"nonce-1","start_generation":"9b2d12d8-7014-4c5e-9d5c-627c2fcc1575","domain":"work","session_id":"123e4567-e89b-42d3-a456-426614174000","backend_kind":"tart","backend_object":"workstation","ca_public_key":"` + key + `","ca_fingerprint":"` + fingerprint + `","principal":"boxwarden-session-123e4567-e89b-42d3-a456-426614174000"}`)
	input.WriteByte('\n')
	return bootstrapper, &input
}
