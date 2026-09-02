package lock

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAcquireGoldenSerializesAndUsesPrivateRegularLock(t *testing.T) {
	root := privateRoot(t)
	first, err := AcquireGolden(context.Background(), root)
	if err != nil {
		t.Fatalf("AcquireGolden first: %v", err)
	}
	defer first.Release()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := AcquireGolden(ctx, root); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("AcquireGolden while held error = %v, want context deadline", err)
	}

	info, err := os.Lstat(filepath.Join(root, "locks", "golden.lock"))
	if err != nil {
		t.Fatalf("Lstat lock: %v", err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		t.Fatalf("lock mode = %v, want regular 0600", info.Mode())
	}
	info, err = os.Lstat(filepath.Join(root, "locks"))
	if err != nil {
		t.Fatalf("Lstat locks directory: %v", err)
	}
	if !info.IsDir() || info.Mode().Perm() != 0o700 {
		t.Fatalf("locks directory mode = %v, want directory 0700", info.Mode())
	}
	if err := first.Release(); err != nil {
		t.Fatalf("Release first: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(root, "locks", "golden.lock")); err != nil {
		t.Fatalf("lock path after release: %v; advisory ownership must not unlink lock files", err)
	}
}

func TestAcquireRejectsUnsafeScopeAndSymlinkedPaths(t *testing.T) {
	root := privateRoot(t)
	if _, err := Acquire(context.Background(), root, "../session"); err == nil {
		t.Fatal("Acquire unsafe scope error = nil, want rejection")
	}

	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "locks")); err != nil {
		t.Fatalf("Symlink locks: %v", err)
	}
	if _, err := AcquireGolden(context.Background(), root); err == nil {
		t.Fatal("AcquireGolden symlinked locks error = nil, want rejection")
	}

	fileRoot := privateRoot(t)
	if err := os.Mkdir(filepath.Join(fileRoot, "locks"), 0o700); err != nil {
		t.Fatalf("Mkdir locks: %v", err)
	}
	if err := os.Symlink(filepath.Join(outside, "lock"), filepath.Join(fileRoot, "locks", "golden.lock")); err != nil {
		t.Fatalf("Symlink golden lock: %v", err)
	}
	if _, err := AcquireGolden(context.Background(), fileRoot); err == nil {
		t.Fatal("AcquireGolden symlinked lock error = nil, want rejection")
	}
}

func TestAcquireSessionUsesValidatedDomainAndSessionScope(t *testing.T) {
	root := privateRoot(t)
	work, err := AcquireSession(context.Background(), root, "work", "dev")
	if err != nil {
		t.Fatalf("AcquireSession work/dev: %v", err)
	}
	defer work.Release()
	personal, err := AcquireSession(context.Background(), root, "personal", "dev")
	if err != nil {
		t.Fatalf("AcquireSession personal/dev: %v", err)
	}
	defer personal.Release()
	for _, name := range []string{"session-work-dev.lock", "session-personal-dev.lock"} {
		if _, err := os.Lstat(filepath.Join(root, "locks", name)); err != nil {
			t.Fatalf("session lock %q: %v", name, err)
		}
	}

	for _, test := range []struct{ domain, session string }{
		{domain: "Work", session: "dev"},
		{domain: "work-name", session: "dev"},
		{domain: "work", session: "Dev"},
		{domain: "work", session: "dev-name"},
		{domain: "../work", session: "dev"},
	} {
		if _, err := AcquireSession(context.Background(), root, test.domain, test.session); err == nil {
			t.Fatalf("AcquireSession(%q, %q) error = nil, want unsafe component rejection", test.domain, test.session)
		}
	}
}

func TestAcquireSessionAcceptsMaximumLengthValidatedComponents(t *testing.T) {
	root := privateRoot(t)
	domain := "a" + strings.Repeat("1", 62)
	name := "b" + strings.Repeat("2", 62)
	held, err := AcquireSession(context.Background(), root, domain, name)
	if err != nil {
		t.Fatalf("AcquireSession maximum-length identities: %v", err)
	}
	defer held.Release()
	if _, err := os.Lstat(filepath.Join(root, "locks", "session-"+domain+"-"+name+".lock")); err != nil {
		t.Fatalf("maximum-length session lock: %v", err)
	}
}

func TestAcquireSyncsStateRootWhenCreatingLockDirectory(t *testing.T) {
	root := privateRoot(t)
	oldSync := syncRoot
	defer func() { syncRoot = oldSync }()
	calls := 0
	syncRoot = func(_ *os.Root) error {
		calls++
		return nil
	}
	held, err := AcquireGolden(context.Background(), root)
	if err != nil {
		t.Fatalf("AcquireGolden: %v", err)
	}
	defer held.Release()
	if calls != 1 {
		t.Fatalf("state-root sync calls = %d, want 1 after first locks directory creation", calls)
	}
}

func TestAcquireFailsClosedWhenValidatedLockDirectoryIsReplaced(t *testing.T) {
	root := privateRoot(t)
	locks := filepath.Join(root, "locks")
	if err := os.Mkdir(locks, 0o700); err != nil {
		t.Fatalf("Mkdir locks: %v", err)
	}
	oldHook := beforeOpenChild
	defer func() { beforeOpenChild = oldHook }()
	beforeOpenChild = func(parent *os.Root, name string) {
		if name != "locks" {
			return
		}
		if err := os.Rename(locks, filepath.Join(root, "locks-original")); err != nil {
			t.Fatalf("Rename validated locks: %v", err)
		}
		if err := os.Mkdir(locks, 0o700); err != nil {
			t.Fatalf("Mkdir replacement locks: %v", err)
		}
	}
	if _, err := AcquireGolden(context.Background(), root); err == nil {
		t.Fatal("AcquireGolden directory replacement error = nil, want closed failure")
	}
	if _, err := os.Lstat(filepath.Join(locks, "golden.lock")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("replacement lock directory unexpectedly used: %v", err)
	}
}

func TestAcquireCanceledContextDoesNotCreateLockState(t *testing.T) {
	root := privateRoot(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := AcquireGolden(ctx, root); !errors.Is(err, context.Canceled) {
		t.Fatalf("AcquireGolden canceled error = %v, want context canceled", err)
	}
	if _, err := os.Lstat(filepath.Join(root, "locks")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("canceled acquisition created lock state: %v", err)
	}
}

func privateRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("EvalSymlinks temp root: %v", err)
	}
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatalf("Chmod root: %v", err)
	}
	return root
}
