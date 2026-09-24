package sshx

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
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

func TestClientSendsOnlyTypedBoundedWorkspaceMountOperations(t *testing.T) {
	connection := testConnection(t)
	runner := &fakeRunner{onRun: func(Command) Result { return Result{Stdout: `{"version":1,"ok":true}`} }}
	client := NewClient(runner)
	mount := WorkspaceMount{VolumeID: "00112233-4455-4677-8899-aabbccddeeff", FilesystemUUID: "10213243-5465-4768-899a-bbccddeeff00", MountPath: "/home/boxwarden/workspaces/project"}
	if err := client.EnsureWorkspaces(context.Background(), connection, []WorkspaceMount{mount}); err != nil {
		t.Fatal(err)
	}
	probe, err := client.Probe(context.Background(), connection, ProbeRequest{Workspaces: []WorkspaceMount{mount}})
	if err != nil || !probe.OK {
		t.Fatalf("bound probe = %+v, %v", probe, err)
	}
	if len(runner.commands) != 2 {
		t.Fatalf("SSH calls = %d", len(runner.commands))
	}
	for i, kind := range []string{"ensure_workspaces", "probe"} {
		command := runner.commands[i]
		if command.Path != sshPath || !sameStrings(command.Args, expectedSSHArgs(connection)) {
			t.Fatalf("unfixed SSH boundary: %+v", command)
		}
		var request managementRequest
		if err := json.Unmarshal(command.Stdin, &request); err != nil || request.Kind != kind || len(request.Workspaces) != 1 || request.Workspaces[0] != mount {
			t.Fatalf("typed request = %+v, %v", request, err)
		}
	}
	before := len(runner.commands)
	for _, invalid := range [][]WorkspaceMount{nil, {{VolumeID: mount.VolumeID, FilesystemUUID: mount.FilesystemUUID, MountPath: "/tmp/unsafe"}}, {mount, mount}} {
		if err := client.EnsureWorkspaces(context.Background(), connection, invalid); err == nil {
			t.Fatalf("unsafe workspace request accepted: %+v", invalid)
		}
	}
	if len(runner.commands) != before {
		t.Fatal("invalid workspace request reached SSH")
	}
}

func TestClientRejectsKnownHostsContentThatDiffersFromDurablePin(t *testing.T) {
	runner := &fakeRunner{onRun: func(Command) Result { return Result{Stdout: `{"version":1,"ok":true}`} }}
	client := NewClient(runner)
	connection := testConnection(t)
	mustWrite(t, connection.KnownHostsFile, []byte(HostKeyAlias(connection.Binding.SessionID)+" "+changedPublicKey+"\n"), 0o600)

	if _, err := client.Probe(context.Background(), connection, ProbeRequest{}); err == nil {
		t.Fatal("Probe() accepted known_hosts content that differs from the durable pin")
	}
	if len(runner.commands) != 0 {
		t.Fatalf("Probe() invoked SSH despite stale known_hosts content: %#v", runner.commands)
	}
}

func TestClientRejectsCertificateOutsideIdentityCompanionPath(t *testing.T) {
	connection := testConnection(t)
	connection.CertificateFile = filepath.Join(connection.RuntimeDirectory, "different-cert.pub")
	mustWrite(t, connection.CertificateFile, []byte("certificate"), 0o644)
	runner := &fakeRunner{onRun: func(Command) Result { return Result{Stdout: `{"version":1,"ok":true}`} }}
	if _, err := NewClient(runner).Probe(context.Background(), connection, ProbeRequest{}); err == nil {
		t.Fatal("Probe accepted a detached certificate path")
	}
	if len(runner.commands) != 0 {
		t.Fatal("Probe invoked SSH with a detached certificate path")
	}
}

func TestClientRejectsOpenSSHPathExpansion(t *testing.T) {
	connection := testConnection(t)
	connection.IdentityFile = filepath.Join(connection.RuntimeDirectory, "${HOME}")
	runner := &fakeRunner{onRun: func(Command) Result { return Result{Stdout: `{"version":1,"ok":true}`} }}
	if _, err := NewClient(runner).Probe(context.Background(), connection, ProbeRequest{}); err == nil {
		t.Fatal("Probe accepted an OpenSSH-expanding path")
	}
	if len(runner.commands) != 0 {
		t.Fatal("Probe invoked SSH with an OpenSSH-expanding path")
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

func TestClientInspectsOnlyExactRequestedPackages(t *testing.T) {
	connection := testConnection(t)
	runner := &fakeRunner{onRun: func(Command) Result {
		return Result{Stdout: `{"version":1,"packages":[{"name":"git","version":"1:2.45.3-1ubuntu2"}]}`}
	}}
	result, err := NewClient(runner).InspectPackages(context.Background(), connection, []string{"git"})
	if err != nil || len(result) != 1 || result[0].Name != "git" || result[0].Version != "1:2.45.3-1ubuntu2" {
		t.Fatalf("InspectPackages() = %#v, %v", result, err)
	}
	var request managementRequest
	if err := json.Unmarshal(runner.commands[0].Stdin, &request); err != nil || request.Kind != "inspect_packages" || !sameStrings(request.Packages, []string{"git"}) {
		t.Fatalf("package request = %#v, %v", request, err)
	}
	for _, response := range []string{
		`{"version":1,"packages":[{"name":"curl","version":"1"}]}`,
		`{"version":1,"packages":[{"name":"git","version":"1","extra":true}]}`,
		`{"version":1,"packages":[{"name":"git","version":""}]}`,
		`{"version":1,"packages":[{"name":"git","version":"1"},{"name":"git","version":"1"}]}`,
	} {
		runner.onRun = func(Command) Result { return Result{Stdout: response} }
		if _, err := NewClient(runner).InspectPackages(context.Background(), connection, []string{"git"}); err == nil {
			t.Fatalf("accepted mismatched package report %q", response)
		}
	}
	before := len(runner.commands)
	if _, err := NewClient(runner).InspectPackages(context.Background(), connection, []string{"git;id"}); err == nil || len(runner.commands) != before {
		t.Fatal("invalid package name reached SSH")
	}
}

func TestClientInspectsFreshGuestIdentityThroughPinnedManagementCommand(t *testing.T) {
	connection := testConnection(t)
	runner := &fakeRunner{onRun: func(Command) Result {
		return Result{Stdout: `{"version":1,"machine_id":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","hostname":"boxwarden-bbbbbbbbbbbb"}`}
	}}
	identity, err := NewClient(runner).InspectIdentity(context.Background(), connection)
	if err != nil || identity.MachineID != strings.Repeat("b", 32) || identity.Hostname != "boxwarden-bbbbbbbbbbbb" {
		t.Fatalf("identity inspection = %+v, %v", identity, err)
	}
	var request managementRequest
	if err := json.Unmarshal(runner.commands[0].Stdin, &request); err != nil || request.Kind != "inspect_identity" || request.Zone != "" || len(request.Packages) != 0 {
		t.Fatalf("identity request = %+v, %v", request, err)
	}
	for _, response := range []string{
		`{"version":1,"machine_id":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","hostname":"boxwarden-bbbbbbbbbbbb","extra":true}`,
		`{"version":1,"machine_id":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","hostname":"boxwarden-aaaaaaaaaaaa"}`,
		`{"version":1,"machine_id":"00000000000000000000000000000000","hostname":"boxwarden-000000000000"}`,
	} {
		runner.onRun = func(Command) Result { return Result{Stdout: response} }
		if _, err := NewClient(runner).InspectIdentity(context.Background(), connection); err == nil {
			t.Fatalf("accepted malformed clone identity %s", response)
		}
	}
}

func testConnection(t *testing.T) Connection {
	t.Helper()
	root := privateRoot(t)
	mustWrite(t, filepath.Join(root, "client"), []byte("client-key"), 0o600)
	mustWrite(t, filepath.Join(root, "client-cert.pub"), []byte("certificate"), 0o644)
	mustWrite(t, filepath.Join(root, "known_hosts"), []byte(HostKeyAlias(testUUID)+" "+testPublicKey+"\n"), 0o600)
	_, _, fingerprint, err := parseEd25519PublicKey(testPublicKey)
	if err != nil {
		t.Fatal(err)
	}
	return Connection{Address: "192.0.2.8", Port: 22, Binding: testBinding(t, testDomain(t, "work", root)), Pin: HostKeyPin{Version: 1, Domain: "work", SessionID: testUUID, BackendKind: "tart", BackendObject: "workstation", Algorithm: "ssh-ed25519", PublicKey: testPublicKey, Fingerprint: fingerprint}, RuntimeDirectory: root, IdentityFile: filepath.Join(root, "client"), CertificateFile: filepath.Join(root, "client-cert.pub"), KnownHostsFile: filepath.Join(root, "known_hosts")}
}

func expectedSSHArgs(connection Connection) []string {
	options := []string{
		"IdentityFile=" + sshQuotedPath(connection.IdentityFile),
		"HostKeyAlias=" + HostKeyAlias(testUUID), "UserKnownHostsFile=" + sshQuotedPath(connection.KnownHostsFile),
		"GlobalKnownHostsFile=/dev/null", "StrictHostKeyChecking=yes", "CheckHostIP=no", "BatchMode=yes",
		"IdentitiesOnly=yes", "IdentityAgent=none", "HostKeyAlgorithms=ssh-ed25519", "UpdateHostKeys=no",
		"PubkeyAcceptedAlgorithms=ssh-ed25519-cert-v01@openssh.com",
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

func TestOpenSSHParsesCredentialPathsContainingSpaces(t *testing.T) {
	if _, err := os.Stat(sshPath); err != nil {
		t.Skipf("OpenSSH unavailable: %v", err)
	}
	root := filepath.Join(t.TempDir(), "Application Support")
	connection := Connection{
		Address: "192.0.2.8", Port: 22,
		Binding:      Binding{SessionID: testUUID},
		IdentityFile: filepath.Join(root, "client"), CertificateFile: filepath.Join(root, "client-cert.pub"), KnownHostsFile: filepath.Join(root, "known_hosts"),
	}
	output, err := exec.Command(sshPath, append([]string{"-G"}, sshArguments(connection)...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("OpenSSH rejected argv: %v: %s", err, output)
	}
	for _, want := range []string{
		"identityfile " + connection.IdentityFile,
		"userknownhostsfile " + connection.KnownHostsFile,
	} {
		if !strings.Contains(string(output), want+"\n") {
			t.Errorf("OpenSSH did not retain exact option %q", want)
		}
	}
}
