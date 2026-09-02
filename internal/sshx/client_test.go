package sshx

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestClientProbeUsesCompleteStrictSSHPolicyAndFixedRemoteCommand(t *testing.T) {
	runner := &fakeRunner{onRun: func(Command) Result { return Result{Stdout: `{"version":1,"ok":true}`} }}
	client := NewClient(runner)
	connection := testConnection(t)
	result, err := client.Probe(context.Background(), connection, ProbeRequest{})
	if err != nil {
		t.Fatalf("Probe() error = %v", err)
	}
	if !result.OK {
		t.Fatalf("Probe() result = %#v", result)
	}
	if len(runner.commands) != 1 {
		t.Fatalf("commands = %#v", runner.commands)
	}
	command := runner.commands[0]
	if command.Path != "/usr/bin/ssh" {
		t.Fatalf("ssh path = %q", command.Path)
	}
	if !sameStrings(command.Args, expectedSSHArgs(connection)) {
		t.Fatalf("ssh argv = %#v\nwant %#v", command.Args, expectedSSHArgs(connection))
	}
	var request managementRequest
	if err := json.Unmarshal(command.Stdin, &request); err != nil {
		t.Fatalf("stdin JSON = %v", err)
	}
	if request.Kind != "probe" || request.Zone != "" || request.Domain != "work" || request.SessionID != testUUID {
		t.Fatalf("stdin request = %#v", request)
	}
}

func TestClientOnlyAcceptsTypedBoundedRequests(t *testing.T) {
	runner := &fakeRunner{onRun: func(command Command) Result {
		if strings.Contains(string(command.Stdin), `"kind":"read_zone"`) {
			return Result{Stdout: `{"version":1,"zone":"America/Chihuahua"}`}
		}
		return Result{Stdout: `{"version":1,"ok":true}`}
	}}
	client := NewClient(runner)
	connection := testConnection(t)
	if err := client.ApplyZone(context.Background(), connection, ApplyZoneRequest{Zone: "America/Chihuahua"}); err != nil {
		t.Fatalf("ApplyZone() error = %v", err)
	}
	zone, err := client.ReadZone(context.Background(), connection, ReadZoneRequest{})
	if err != nil || zone != "America/Chihuahua" {
		t.Fatalf("ReadZone() = %q, %v", zone, err)
	}
	if err := client.ApplyZone(context.Background(), connection, ApplyZoneRequest{Zone: "../../unsafe"}); err == nil {
		t.Fatal("ApplyZone(malformed zone) error = nil")
	}
	if len(runner.commands) != 2 {
		t.Fatalf("expected exactly typed request calls, got %#v", runner.commands)
	}
}

func testConnection(t *testing.T) Connection {
	t.Helper()
	root := privateRoot(t)
	mustWrite(t, filepath.Join(root, "client"), []byte("client-key"), 0o600)
	mustWrite(t, filepath.Join(root, "client-cert.pub"), []byte("certificate"), 0o644)
	mustWrite(t, filepath.Join(root, "known_hosts"), []byte("known-host"), 0o600)
	_, _, fingerprint, err := parseEd25519PublicKey(testPublicKey)
	if err != nil {
		t.Fatal(err)
	}
	return Connection{Address: "192.0.2.8", Port: 22, Binding: testBinding(t, testDomain(t, "work", root)), Pin: HostKeyPin{Version: 1, Domain: "work", SessionID: testUUID, BackendKind: "tart", BackendObject: "workstation", Algorithm: "ssh-ed25519", PublicKey: testPublicKey, Fingerprint: fingerprint}, RuntimeDirectory: root, IdentityFile: filepath.Join(root, "client"), CertificateFile: filepath.Join(root, "client-cert.pub"), KnownHostsFile: filepath.Join(root, "known_hosts")}
}

func expectedSSHArgs(connection Connection) []string {
	options := []string{
		"IdentityFile=" + connection.IdentityFile, "CertificateFile=" + connection.CertificateFile,
		"HostKeyAlias=" + HostKeyAlias(testUUID), "UserKnownHostsFile=" + connection.KnownHostsFile,
		"GlobalKnownHostsFile=/dev/null", "StrictHostKeyChecking=yes", "CheckHostIP=no", "BatchMode=yes",
		"IdentitiesOnly=yes", "IdentityAgent=none", "HostKeyAlgorithms=ssh-ed25519", "UpdateHostKeys=no",
		"VerifyHostKeyDNS=no", "CanonicalizeHostname=no", "ProxyCommand=none", "ProxyJump=none",
		"ControlMaster=no", "ControlPath=none", "RequestTTY=no", "PasswordAuthentication=no",
		"KbdInteractiveAuthentication=no", "ForwardAgent=no", "ForwardX11=no", "ClearAllForwardings=yes",
		"PermitLocalCommand=no", "Tunnel=no", "ConnectTimeout=10", "ServerAliveInterval=5", "ServerAliveCountMax=3",
	}
	args := []string{"-F", "/dev/null"}
	for _, option := range options {
		args = append(args, "-o", option)
	}
	args = append(args, "-p", "22", "boxwarden@192.0.2.8", "/usr/bin/sudo", "-n", "--", "/usr/local/libexec/boxwarden-guest-bootstrap", "management")
	return args
}
