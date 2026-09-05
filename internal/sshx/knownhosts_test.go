package sshx

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteKnownHostsUsesOnlyUUIDDerivedAlias(t *testing.T) {
	runtime := privateRoot(t)
	_, _, fingerprint, err := parseEd25519PublicKey(testPublicKey)
	if err != nil {
		t.Fatal(err)
	}
	pin := HostKeyPin{Version: 1, Domain: testDomain(t, "work", privateRoot(t)).ID, SessionID: testUUID, BackendKind: "tart", BackendObject: "workstation", Algorithm: "ssh-ed25519", PublicKey: testPublicKey, Fingerprint: fingerprint}
	path, err := WriteKnownHosts(runtime, pin)
	if err != nil {
		t.Fatalf("WriteKnownHosts() error = %v", err)
	}
	if filepath.Base(path) != "known_hosts" {
		t.Fatalf("known hosts path = %q", path)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := HostKeyAlias(testUUID) + " " + testPublicKey + "\n"
	if string(contents) != want {
		t.Fatalf("known_hosts = %q, want %q", contents, want)
	}
}
