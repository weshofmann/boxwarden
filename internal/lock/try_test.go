package lock

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestTryAcquireRefusesBusyImmediatelyAndReacquires(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	first, err := Acquire(context.Background(), root, "clipboard")
	if err != nil {
		t.Fatal(err)
	}
	defer first.Release()
	started := time.Now()
	second, err := TryAcquire(context.Background(), root, "clipboard")
	if second != nil || !errors.Is(err, ErrBusy) {
		t.Fatalf("busy attempt = %v, %v", second, err)
	}
	if time.Since(started) > time.Second {
		t.Fatal("busy request waited")
	}
	if err := first.Release(); err != nil {
		t.Fatal(err)
	}
	third, err := TryAcquire(context.Background(), root, "clipboard")
	if err != nil {
		t.Fatal(err)
	}
	defer third.Release()
	if !third.MatchesExact(root, "clipboard") {
		t.Fatal("new handle does not own exact scope")
	}
}
func TestTryAcquireCancelledDoesNotCreateLock(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	held, err := TryAcquire(ctx, root, "clipboard")
	if held != nil || !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled acquisition accepted")
	}
	if _, err := os.Stat(filepath.Join(root, "locks")); !os.IsNotExist(err) {
		t.Fatal("cancelled attempt created lock directory")
	}
}
