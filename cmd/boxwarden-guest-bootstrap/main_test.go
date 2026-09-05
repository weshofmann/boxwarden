package main

import (
	"bytes"
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

func (errorWriter) Write([]byte) (int, error) { return 0, errors.New("write failed") }

type shortOutputWriter struct{}

func (shortOutputWriter) Write(data []byte) (int, error) { return len(data) - 1, nil }

// This fails if helper success is reported after the serial response could not
// reach the host broker.
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
