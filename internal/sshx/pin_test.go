package sshx

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestPinStoreAdmitIsImmutableAndBindingBound(t *testing.T) {
	root := privateRoot(t)
	work := testDomain(t, "work", root)
	binding := testBinding(t, work)
	store := NewPinStore(work)
	key := ObservedHostKey{Algorithm: "ssh-ed25519", PublicKey: testPublicKey}
	pin, err := store.Admit(context.Background(), binding, key)
	if err != nil {
		t.Fatalf("Admit() error = %v", err)
	}
	if pin.Domain != work.ID || pin.SessionID != testUUID || pin.BackendKind != "tart" || pin.PublicKey != testPublicKey {
		t.Fatalf("pin = %#v", pin)
	}
	if _, err := store.Admit(context.Background(), binding, key); err != nil {
		t.Fatalf("Admit(exact retry) error = %v", err)
	}
	if _, err := store.Admit(context.Background(), binding, ObservedHostKey{Algorithm: "ssh-ed25519", PublicKey: changedPublicKey}); err == nil {
		t.Fatal("Admit(changed key) error = nil")
	}
	path := filepath.Join(root, "identity", "ssh-host-pins", testUUID+".json")
	if info, err := os.Lstat(path); err != nil || info.Mode().Perm() != 0o600 || info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("pin state = %v, %v", info, err)
	}
}

func TestPinStoreRejectsInvalidKeyAndUnsafeStoredPin(t *testing.T) {
	root := privateRoot(t)
	work := testDomain(t, "work", root)
	store := NewPinStore(work)
	binding := testBinding(t, work)
	if _, err := store.Admit(context.Background(), binding, ObservedHostKey{Algorithm: "rsa", PublicKey: testPublicKey}); err == nil {
		t.Fatal("Admit(rsa) error = nil")
	}
	if _, err := store.Admit(context.Background(), binding, ObservedHostKey{Algorithm: "ssh-ed25519", PublicKey: "ssh-ed25519 not-base64"}); err == nil {
		t.Fatal("Admit(malformed) error = nil")
	}
	if err := os.MkdirAll(filepath.Join(root, "identity", "ssh-host-pins"), 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "identity", "ssh-host-pins", testUUID+".json")
	if err := os.Symlink("elsewhere", path); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(context.Background(), binding); err == nil {
		t.Fatal("Load(symlink) error = nil")
	}
}
