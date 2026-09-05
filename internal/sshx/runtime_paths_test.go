package sshx

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestRuntimeCredentialTreeRejectsUnsafeIntermediateAndACL(t *testing.T) {
	root := privateRoot(t)
	nested := filepath.Join(root, "nested")
	if err := os.Mkdir(nested, 0o700); err != nil {
		t.Fatal(err)
	}
	credential := filepath.Join(nested, "client")
	mustWrite(t, credential, []byte("client"), 0o600)
	if err := requirePrivateTree(root, nested); err != nil {
		t.Fatalf("requirePrivateTree() error = %v", err)
	}
	if err := os.Chmod(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := requirePrivateTree(root, nested); err == nil {
		t.Fatal("requirePrivateTree() accepted unsafe intermediate directory")
	}
	if err := os.Chmod(nested, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := requirePrivateTreeWithACL(root, nested, privateACLInspectorFake{extended: true}); err == nil {
		t.Fatal("requirePrivateTreeWithACL() accepted ACL-bearing intermediate directory")
	}
}

func TestClientRejectsNestedRuntimeCredentialAncestor(t *testing.T) {
	root := privateRoot(t)
	runtime := filepath.Join(root, "runtime")
	if err := os.Mkdir(runtime, 0o700); err != nil {
		t.Fatal(err)
	}
	connection := testConnection(t)
	for _, pair := range [][2]string{{connection.IdentityFile, filepath.Join(runtime, "client")}, {connection.CertificateFile, filepath.Join(runtime, "client-cert.pub")}, {connection.KnownHostsFile, filepath.Join(runtime, "known_hosts")}} {
		contents, err := os.ReadFile(pair[0])
		if err != nil {
			t.Fatal(err)
		}
		info, err := os.Stat(pair[0])
		if err != nil {
			t.Fatal(err)
		}
		mustWrite(t, pair[1], contents, info.Mode().Perm())
	}
	connection.RuntimeDirectory = root
	connection.IdentityFile = filepath.Join(runtime, "client")
	connection.CertificateFile = filepath.Join(runtime, "client-cert.pub")
	connection.KnownHostsFile = filepath.Join(runtime, "known_hosts")
	if err := os.Chmod(runtime, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := NewClient(&fakeRunner{onRun: func(Command) Result { return Result{Stdout: `{"version":1,"ok":true}`} }}).Probe(context.Background(), connection, ProbeRequest{}); err == nil {
		t.Fatal("Probe() accepted unsafe nested runtime directory")
	}
}

func TestRuntimeCredentialTreeRejectsIntermediateSymlink(t *testing.T) {
	root, target := privateRoot(t), privateRoot(t)
	if err := os.Symlink(target, filepath.Join(root, "nested")); err != nil {
		t.Fatal(err)
	}
	if err := requirePrivateTree(root, filepath.Join(root, "nested")); err == nil {
		t.Fatal("requirePrivateTree() accepted intermediate symlink")
	}
}
