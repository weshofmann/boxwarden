package sshx

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/weshofmann/boxwarden/internal/domain"
)

func TestCAStoreCheckReportsEntirelyAbsentCAWithoutMutatingState(t *testing.T) {
	root := privateRoot(t)
	work := testDomain(t, "work", root)
	runner := newKeygenRunner(t)
	store := NewCAStore(CAStoreOptions{Runner: runner, Identity: StaticIdentity{UID: 501, Name: "wes"}})

	_, err := store.Check(context.Background(), work, []Domain{work})
	if !errors.Is(err, ErrCAMissing) {
		t.Fatalf("Check(absent) error = %v, want errors.Is(ErrCAMissing)", err)
	}
	if _, err := os.Lstat(filepath.Join(root, "identity")); !os.IsNotExist(err) {
		t.Fatalf("Check(absent) created or changed identity directory: %v", err)
	}
	if len(runner.commands) != 0 {
		t.Fatalf("Check(absent) invoked ssh-keygen: %#v", runner.commands)
	}
}

func TestCAStorePublicOperationsCapWallDeadlineAndPreserveShorterCallerDeadline(t *testing.T) {
	root := privateRoot(t)
	work := testDomain(t, "work", root)
	runner := &deadlineCapturingRunner{Runner: newKeygenRunner(t)}
	store := NewCAStore(CAStoreOptions{Runner: runner, Identity: StaticIdentity{UID: 501, Name: "wes"}, NewUUID: func() (string, error) { return testUUID, nil }})
	if _, err := store.Init(context.Background(), work, []Domain{work}); err != nil {
		t.Fatal(err)
	}
	assertDeadlineWithin(t, runner.deadlines, caWallTimeout)

	runner.deadlines = nil
	if _, err := store.Check(context.Background(), work, []Domain{work}); err != nil {
		t.Fatal(err)
	}
	assertDeadlineWithin(t, runner.deadlines, caWallTimeout)

	runner.deadlines = nil
	shortContext, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := store.Load(shortContext, work); err != nil {
		t.Fatal(err)
	}
	assertDeadlineWithin(t, runner.deadlines, time.Second)
}

type deadlineCapturingRunner struct {
	Runner
	deadlines []time.Time
}

func (r *deadlineCapturingRunner) Run(ctx context.Context, command Command) (Result, error) {
	deadline, ok := ctx.Deadline()
	if !ok {
		return Result{}, fmt.Errorf("CA runner context has no deadline")
	}
	r.deadlines = append(r.deadlines, deadline)
	return r.Runner.Run(ctx, command)
}

func assertDeadlineWithin(t *testing.T, deadlines []time.Time, maximum time.Duration) {
	t.Helper()
	if len(deadlines) == 0 {
		t.Fatal("operation did not invoke its runner")
	}
	for _, deadline := range deadlines {
		if until := time.Until(deadline); until > maximum+time.Second {
			t.Fatalf("deadline exceeds cap: %v", until)
		}
	}
}

func TestCAStoreCheckAcceptsCompleteDomainBoundCA(t *testing.T) {
	root := privateRoot(t)
	work := testDomain(t, "work", root)
	runner := newKeygenRunner(t)
	store := NewCAStore(CAStoreOptions{Runner: runner, Identity: StaticIdentity{UID: 501, Name: "wes"}, NewUUID: func() (string, error) { return testUUID, nil }})
	initialized, err := store.Init(context.Background(), work, []Domain{work})
	if err != nil {
		t.Fatal(err)
	}
	runner.commands = nil

	checked, err := store.Check(context.Background(), work, []Domain{work})
	if err != nil {
		t.Fatalf("Check(complete) error = %v", err)
	}
	if checked.Domain != work.ID || checked.Fingerprint != initialized.Fingerprint || checked.CreationUUID != initialized.CreationUUID {
		t.Fatalf("Check(complete) = %#v, want validated initialized identity", checked)
	}
	if len(runner.commands) != 1 || len(runner.commands[0].Args) == 0 || runner.commands[0].Args[0] != "-y" {
		t.Fatalf("Check(complete) runner commands = %#v, want one private/public validation", runner.commands)
	}
}

func TestCAStoreCheckRejectsPartialCAState(t *testing.T) {
	root := privateRoot(t)
	work := testDomain(t, "work", root)
	dir := filepath.Join(root, "identity", "ssh-user-ca")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(dir, "ca"), []byte("private"), 0o600)
	store := NewCAStore(CAStoreOptions{Runner: newKeygenRunner(t), Identity: StaticIdentity{UID: 501, Name: "wes"}})

	_, err := store.Check(context.Background(), work, []Domain{work})
	if err == nil || errors.Is(err, ErrCAMissing) {
		t.Fatalf("Check(partial) error = %v, want unsafe non-missing error", err)
	}
}

func TestCAStoreCheckScansConfiguredPartialStateBeforeReportingSelectedMissing(t *testing.T) {
	workRoot, personalRoot := privateRoot(t), privateRoot(t)
	work, personal := testDomain(t, "work", workRoot), testDomain(t, "personal", personalRoot)
	dir := filepath.Join(personalRoot, "identity", "ssh-user-ca")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(dir, "ca"), []byte("private"), 0o600)
	store := NewCAStore(CAStoreOptions{Runner: newKeygenRunner(t), Identity: StaticIdentity{UID: 501, Name: "wes"}})

	_, err := store.Check(context.Background(), work, []Domain{work, personal})
	if err == nil || errors.Is(err, ErrCAMissing) {
		t.Fatalf("Check(absent selected with partial configured domain) error = %v, want configured-state error", err)
	}
}

func TestCAStoreInitRejectsConfiguredPartialStateBeforeCreatingSelectedCA(t *testing.T) {
	workRoot, personalRoot := privateRoot(t), privateRoot(t)
	work, personal := testDomain(t, "work", workRoot), testDomain(t, "personal", personalRoot)
	dir := filepath.Join(personalRoot, "identity", "ssh-user-ca")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(dir, "ca"), []byte("private"), 0o600)
	runner := newKeygenRunner(t)
	store := NewCAStore(CAStoreOptions{Runner: runner, Identity: StaticIdentity{UID: 501, Name: "wes"}, NewUUID: func() (string, error) { return testUUID, nil }})

	if _, err := store.Init(context.Background(), work, []Domain{work, personal}); err == nil {
		t.Fatal("Init(absent selected with partial configured domain) error = nil")
	}
	if _, err := os.Lstat(filepath.Join(workRoot, "identity")); !os.IsNotExist(err) {
		t.Fatalf("Init(configured partial state) created selected identity directory: %v", err)
	}
	if len(runner.commands) != 0 {
		t.Fatalf("Init(configured partial state) invoked ssh-keygen: %#v", runner.commands)
	}
}

func TestCAStoreCheckScansConfiguredDuplicateBeforeReportingSelectedMissing(t *testing.T) {
	workRoot, personalRoot, otherRoot := privateRoot(t), privateRoot(t), privateRoot(t)
	work, personal, other := testDomain(t, "work", workRoot), testDomain(t, "personal", personalRoot), testDomain(t, "other", otherRoot)
	store := NewCAStore(CAStoreOptions{Runner: newKeygenRunner(t), Identity: StaticIdentity{UID: 501, Name: "wes"}, NewUUID: func() (string, error) { return testUUID, nil }})
	identity, err := store.Init(context.Background(), personal, []Domain{personal})
	if err != nil {
		t.Fatal(err)
	}
	copyCAState(t, personalRoot, otherRoot)
	identity.Domain = other.ID
	metadata, err := json.Marshal(identity)
	if err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(otherRoot, "identity", "ssh-user-ca", "metadata.json"), append(metadata, '\n'), 0o600)

	_, err = store.Check(context.Background(), work, []Domain{work, personal, other})
	if err == nil || errors.Is(err, ErrCAMissing) {
		t.Fatalf("Check(absent selected with duplicate configured domains) error = %v, want duplicate error", err)
	}
}

func TestCAStoreCheckRejectsDuplicateConfiguredDomainFingerprint(t *testing.T) {
	workRoot, personalRoot := privateRoot(t), privateRoot(t)
	work, personal := testDomain(t, "work", workRoot), testDomain(t, "personal", personalRoot)
	runner := newKeygenRunner(t)
	store := NewCAStore(CAStoreOptions{Runner: runner, Identity: StaticIdentity{UID: 501, Name: "wes"}, NewUUID: func() (string, error) { return testUUID, nil }})
	identity, err := store.Init(context.Background(), work, []Domain{work})
	if err != nil {
		t.Fatal(err)
	}
	copyCAState(t, workRoot, personalRoot)
	identity.Domain = personal.ID
	metadata, err := json.Marshal(identity)
	if err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(personalRoot, "identity", "ssh-user-ca", "metadata.json"), append(metadata, '\n'), 0o600)

	if _, err := store.Check(context.Background(), work, []Domain{work, personal}); err == nil {
		t.Fatal("Check(duplicate configured fingerprint) error = nil")
	}
}

func TestCAStoreCheckRejectsCopiedCADomainMismatch(t *testing.T) {
	workRoot, personalRoot := privateRoot(t), privateRoot(t)
	work, personal := testDomain(t, "work", workRoot), testDomain(t, "personal", personalRoot)
	store := NewCAStore(CAStoreOptions{Runner: newKeygenRunner(t), Identity: StaticIdentity{UID: 501, Name: "wes"}, NewUUID: func() (string, error) { return testUUID, nil }})
	if _, err := store.Init(context.Background(), work, []Domain{work}); err != nil {
		t.Fatal(err)
	}
	copyCAState(t, workRoot, personalRoot)

	if _, err := store.Check(context.Background(), personal, []Domain{work, personal}); err == nil {
		t.Fatal("Check(copied CA/domain mismatch) error = nil")
	}
}

func TestCAStoreCheckDoesNotFallBackToAnotherDomainCA(t *testing.T) {
	workRoot, personalRoot := privateRoot(t), privateRoot(t)
	work, personal := testDomain(t, "work", workRoot), testDomain(t, "personal", personalRoot)
	runner := newKeygenRunner(t)
	store := NewCAStore(CAStoreOptions{Runner: runner, Identity: StaticIdentity{UID: 501, Name: "wes"}, NewUUID: func() (string, error) { return testUUID, nil }})
	if _, err := store.Init(context.Background(), personal, []Domain{personal}); err != nil {
		t.Fatal(err)
	}
	runner.commands = nil

	_, err := store.Check(context.Background(), work, []Domain{work, personal})
	if !errors.Is(err, ErrCAMissing) {
		t.Fatalf("Check(absent selected domain with another valid CA) error = %v, want ErrCAMissing", err)
	}
	if len(runner.commands) != 1 || len(runner.commands[0].Args) == 0 || runner.commands[0].Args[0] != "-y" {
		t.Fatalf("Check(absent selected domain) runner commands = %#v, want configured-domain validation only", runner.commands)
	}
}

func copyCAState(t *testing.T, sourceRoot, destinationRoot string) {
	t.Helper()
	destination := filepath.Join(destinationRoot, "identity", "ssh-user-ca")
	if err := os.MkdirAll(destination, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, file := range []struct {
		name string
		mode os.FileMode
	}{
		{name: "ca", mode: 0o600},
		{name: "ca.pub", mode: 0o644},
		{name: "metadata.json", mode: 0o600},
	} {
		contents, err := os.ReadFile(filepath.Join(sourceRoot, "identity", "ssh-user-ca", file.name))
		if err != nil {
			t.Fatal(err)
		}
		mustWrite(t, filepath.Join(destination, file.name), contents, file.mode)
	}
}

func TestCAStoreInitReturnsUUIDFailureWithoutCreatingCAState(t *testing.T) {
	root := privateRoot(t)
	work := testDomain(t, "work", root)
	wantErr := errors.New("UUID source unavailable")
	runner := newKeygenRunner(t)
	store := NewCAStore(CAStoreOptions{
		Runner:   runner,
		Identity: StaticIdentity{UID: 501, Name: "wes"},
		NewUUID: func() (string, error) {
			return "", wantErr
		},
	})

	_, err := store.Init(context.Background(), work, []Domain{work})
	if !errors.Is(err, wantErr) {
		t.Fatalf("Init(UUID failure) error = %v, want errors.Is(%v)", err, wantErr)
	}
	if _, err := os.Lstat(filepath.Join(root, "identity")); !os.IsNotExist(err) {
		t.Fatalf("Init(UUID failure) created CA state: %v", err)
	}
	if len(runner.commands) != 0 {
		t.Fatalf("Init(UUID failure) invoked ssh-keygen: %#v", runner.commands)
	}
}

func TestCAStoreInitReturnsOperatorFailureWithoutCreatingCAState(t *testing.T) {
	root := privateRoot(t)
	work := testDomain(t, "work", root)
	wantErr := errors.New("operator identity unavailable")
	runner := newKeygenRunner(t)
	store := NewCAStore(CAStoreOptions{
		Runner:   runner,
		Identity: failingIdentity{err: wantErr},
		NewUUID:  func() (string, error) { return testUUID, nil },
	})

	_, err := store.Init(context.Background(), work, []Domain{work})
	if !errors.Is(err, wantErr) {
		t.Fatalf("Init(operator failure) error = %v, want errors.Is(%v)", err, wantErr)
	}
	if _, err := os.Lstat(filepath.Join(root, "identity")); !os.IsNotExist(err) {
		t.Fatalf("Init(operator failure) created CA state: %v", err)
	}
	if len(runner.commands) != 0 {
		t.Fatalf("Init(operator failure) invoked ssh-keygen: %#v", runner.commands)
	}
}

type failingIdentity struct{ err error }

func (i failingIdentity) Current(context.Context) (Operator, error) { return Operator{}, i.err }

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
	store := NewCAStore(CAStoreOptions{Runner: runner, Identity: StaticIdentity{UID: 501, Name: "wes"}, NewUUID: func() (string, error) { return testUUID, nil }})

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
	store := NewCAStore(CAStoreOptions{Runner: runner, Identity: StaticIdentity{UID: 501, Name: "wes"}, NewUUID: func() (string, error) { return testUUID, nil }})
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
	store := NewCAStore(CAStoreOptions{Runner: newKeygenRunner(t), Identity: StaticIdentity{UID: 501, Name: "wes"}, NewUUID: func() (string, error) { return testUUID, nil }})
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
	store := NewCAStore(CAStoreOptions{Runner: newKeygenRunner(t), Identity: StaticIdentity{UID: 501, Name: "wes"}, NewUUID: func() (string, error) { return testUUID, nil }})
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
