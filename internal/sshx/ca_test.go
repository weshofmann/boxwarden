package sshx

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/weshofmann/boxwarden/internal/domain"
)

func TestCAStoreInitCreatesOneDomainBoundCA(t *testing.T) {
	root := privateRoot(t)
	work := testDomain(t, "work", root)
	runner := &fakeRunner{onRun: func(command Command) Result {
		if command.Path == "/usr/bin/ssh-keygen" && len(command.Args) > 0 && command.Args[0] == "-q" {
			keyPath := command.Args[4]
			mustWrite(t, keyPath, []byte("private-ca"), 0o600)
			mustWrite(t, keyPath+".pub", []byte(testPublicKey+" boxwarden-ca\n"), 0o600)
		}
		if len(command.Args) > 0 && command.Args[0] == "-y" {
			return Result{Stdout: testPublicKey + " boxwarden-ca\n"}
		}
		return Result{}
	}}
	store := NewCAStore(CAStoreOptions{Runner: runner, Identity: StaticIdentity{UID: 501, Name: "wes"}, NewUUID: func() string { return testUUID }})

	identity, err := store.Init(context.Background(), work, []Domain{work})
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	if identity.Domain != work.ID || identity.Algorithm != "ssh-ed25519" || identity.CreationUUID != testUUID {
		t.Fatalf("Init() identity = %#v", identity)
	}
	if len(runner.commands) != 3 {
		t.Fatalf("ssh-keygen calls = %d, want create and metadata revalidation", len(runner.commands))
	}
	if got, want := runner.commands[0].Args, []string{"-q", "-t", "ed25519", "-f", filepath.Join(root, "identity", "ssh-user-ca", "ca"), "-N", "", "-C", "boxwarden:work:management-ca"}; !sameStrings(got, want) {
		t.Fatalf("create argv = %#v, want %#v", got, want)
	}
	info, err := os.Lstat(filepath.Join(root, "identity", "ssh-user-ca", "ca"))
	if err != nil || info.Mode().Perm() != 0o600 || info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("private CA mode/state = %v, %v", info, err)
	}
}

func TestCAStoreRejectsCopiedTreeAndCrossDomainReuse(t *testing.T) {
	workRoot, personalRoot := privateRoot(t), privateRoot(t)
	work, personal := testDomain(t, "work", workRoot), testDomain(t, "personal", personalRoot)
	runner := newKeygenRunner(t)
	store := NewCAStore(CAStoreOptions{Runner: runner, Identity: StaticIdentity{UID: 501, Name: "wes"}, NewUUID: func() string { return testUUID }})
	if _, err := store.Init(context.Background(), work, []Domain{work}); err != nil {
		t.Fatalf("initialize work: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(personalRoot, "identity"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(workRoot, "identity", "ssh-user-ca"), filepath.Join(personalRoot, "identity", "ssh-user-ca")); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(context.Background(), personal); err == nil {
		t.Fatal("Load(copied/symlinked CA) error = nil")
	}
	if err := os.Remove(filepath.Join(personalRoot, "identity", "ssh-user-ca")); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Init(context.Background(), personal, []Domain{work, personal}); err == nil {
		t.Fatal("Init() duplicate configured-domain fingerprint error = nil")
	}
}

func TestCAStoreRejectsPartialAndHardlinkedState(t *testing.T) {
	root := privateRoot(t)
	work := testDomain(t, "work", root)
	dir := filepath.Join(root, "identity", "ssh-user-ca")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(dir, "ca"), []byte("private"), 0o600)
	store := NewCAStore(CAStoreOptions{Runner: newKeygenRunner(t), Identity: StaticIdentity{UID: 501, Name: "wes"}, NewUUID: func() string { return testUUID }})
	if _, err := store.Init(context.Background(), work, []Domain{work}); err == nil {
		t.Fatal("Init(partial) error = nil")
	}
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Init(context.Background(), work, []Domain{work}); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(filepath.Join(dir, "ca.pub"), filepath.Join(dir, "copy.pub")); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(context.Background(), work); err == nil {
		t.Fatal("Load(hardlinked public key) error = nil")
	}
}

func TestCAStoreLoadRejectsSymlinkedIdentityAncestor(t *testing.T) {
	root, elsewhere := privateRoot(t), privateRoot(t)
	work := testDomain(t, "work", root)
	store := NewCAStore(CAStoreOptions{Runner: newKeygenRunner(t), Identity: StaticIdentity{UID: 501, Name: "wes"}, NewUUID: func() string { return testUUID }})
	if _, err := store.Init(context.Background(), work, []Domain{work}); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(root, "identity"), filepath.Join(elsewhere, "identity")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(elsewhere, "identity"), filepath.Join(root, "identity")); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(context.Background(), work); err == nil {
		t.Fatal("Load(symlinked identity ancestor) error = nil")
	}
}

func testDomain(t *testing.T, raw, root string) Domain {
	t.Helper()
	id, err := domain.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return Domain{ID: id, StateRoot: root}
}
