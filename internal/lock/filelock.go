// Package lock provides owner-private advisory operation locks.
//
// When an operation needs both a session lock and a golden lock, it must acquire
// the session lock first and then the golden lock. Golden-only operations acquire
// only the latter. Keeping that order prevents future lifecycle code from
// introducing a lock cycle with golden registration or resolution.
package lock

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"syscall"
	"time"
)

// A session scope contains the fixed prefix plus two independently validated
// 63-byte identities. The result remains below the portable 255-byte filename
// component ceiling after the .lock suffix is added.
const maxScopeLength = 250

var (
	syncRoot        = syncRootDirectory
	beforeOpenChild = func(*os.Root, string) {}
)

// Held is an advisory lock retained until Release. Its file descriptor remains
// open while held; lock-file existence is never used as ownership evidence.
type Held struct {
	file *os.File
	once sync.Once
	err  error
}

// Acquire obtains an owner-private lock named by a safe scope below stateRoot.
func Acquire(ctx context.Context, stateRoot, scope string) (*Held, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := validScope(scope); err != nil {
		return nil, err
	}
	root, err := openStateRoot(stateRoot)
	if err != nil {
		return nil, fmt.Errorf("state root: %w", err)
	}
	defer root.Close()
	locks, err := openPrivateChild(root, "locks", true)
	if err != nil {
		return nil, fmt.Errorf("lock directory: %w", err)
	}
	defer locks.Close()

	name := scope + ".lock"
	info, err := locks.Lstat(name)
	if err == nil {
		if err := requirePrivateRegular(info); err != nil {
			return nil, fmt.Errorf("lock %q: %w", scope, err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("inspect lock %q: %w", scope, err)
	}
	file, err := locks.OpenFile(name, os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open lock %q: %w", scope, err)
	}
	opened, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, fmt.Errorf("inspect opened lock %q: %w", scope, err)
	}
	if err := requirePrivateRegular(opened); err != nil {
		file.Close()
		return nil, fmt.Errorf("lock %q: %w", scope, err)
	}
	if info != nil && !os.SameFile(info, opened) {
		file.Close()
		return nil, fmt.Errorf("lock %q changed while opening", scope)
	}

	for {
		err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return &Held{file: file}, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) {
			file.Close()
			return nil, fmt.Errorf("acquire lock %q: %w", scope, err)
		}
		select {
		case <-ctx.Done():
			file.Close()
			return nil, ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
}

// AcquireGolden obtains the domain's golden lock.
func AcquireGolden(ctx context.Context, stateRoot string) (*Held, error) {
	return Acquire(ctx, stateRoot, "golden")
}

// AcquireSession obtains a lock scoped to one validated domain and session. An
// operation requiring a golden lock acquires this session lock first.
func AcquireSession(ctx context.Context, stateRoot, domain, name string) (*Held, error) {
	if !validDomain(domain) {
		return nil, fmt.Errorf("invalid lock domain %q", domain)
	}
	if !validSessionName(name) {
		return nil, fmt.Errorf("invalid lock session name %q", name)
	}
	return Acquire(ctx, stateRoot, "session-"+domain+"-"+name)
}

// Release releases the advisory lock and closes its descriptor. It is idempotent.
func (h *Held) Release() error {
	if h == nil || h.file == nil {
		return nil
	}
	h.once.Do(func() { h.err = h.file.Close() })
	return h.err
}

func openStateRoot(path string) (*os.Root, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if err := requirePrivateDirectory(info); err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		return nil, err
	}
	opened, err := root.Stat(".")
	if err != nil {
		root.Close()
		return nil, err
	}
	if !os.SameFile(info, opened) {
		root.Close()
		return nil, fmt.Errorf("changed while opening")
	}
	return root, nil
}

func openPrivateChild(parent *os.Root, name string, create bool) (*os.Root, error) {
	info, err := parent.Lstat(name)
	if errors.Is(err, os.ErrNotExist) && create {
		if err := parent.Mkdir(name, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		if err := syncRoot(parent); err != nil {
			return nil, fmt.Errorf("sync parent after create: %w", err)
		}
		info, err = parent.Lstat(name)
	}
	if err != nil {
		return nil, err
	}
	if err := requirePrivateDirectory(info); err != nil {
		return nil, err
	}
	beforeOpenChild(parent, name)
	child, err := parent.OpenRoot(name)
	if err != nil {
		return nil, err
	}
	opened, err := child.Stat(".")
	if err != nil {
		child.Close()
		return nil, err
	}
	if !os.SameFile(info, opened) {
		child.Close()
		return nil, fmt.Errorf("changed while opening")
	}
	return child, nil
}

func syncRootDirectory(root *os.Root) error {
	file, err := root.Open(".")
	if err != nil {
		return err
	}
	defer file.Close()
	return file.Sync()
}

func requirePrivateDirectory(info os.FileInfo) error {
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("must be a non-symlink directory")
	}
	if info.Mode().Perm() != 0o700 {
		return fmt.Errorf("must have mode 0700")
	}
	return nil
}

func requirePrivateRegular(info os.FileInfo) error {
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("must be a regular non-symlink file")
	}
	if info.Mode().Perm() != 0o600 {
		return fmt.Errorf("must have mode 0600")
	}
	return nil
}

func validScope(scope string) error {
	if len(scope) == 0 || len(scope) > maxScopeLength || !isAlphaNumeric(scope[0]) {
		return fmt.Errorf("invalid lock scope %q", scope)
	}
	for index := 1; index < len(scope); index++ {
		if !isAlphaNumeric(scope[index]) && scope[index] != '-' {
			return fmt.Errorf("invalid lock scope %q", scope)
		}
	}
	return nil
}

func isAlphaNumeric(value byte) bool {
	return (value >= 'a' && value <= 'z') || (value >= 'A' && value <= 'Z') || (value >= '0' && value <= '9')
}

func validDomain(raw string) bool      { return validLowerIdentifier(raw) }
func validSessionName(raw string) bool { return validLowerIdentifier(raw) }

func validLowerIdentifier(raw string) bool {
	if len(raw) == 0 || len(raw) > 63 || raw[0] < 'a' || raw[0] > 'z' {
		return false
	}
	for index := 1; index < len(raw); index++ {
		if (raw[index] < 'a' || raw[index] > 'z') && (raw[index] < '0' || raw[index] > '9') {
			return false
		}
	}
	return true
}
