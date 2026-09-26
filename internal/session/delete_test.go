package session

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/weshofmann/boxwarden/internal/backend"
	"github.com/weshofmann/boxwarden/internal/domain"
)

func TestDeleteRetryResyncsVisibleIntentBeforeBackendMutation(t *testing.T) {
	configured, backendFake, creator := createFixture(t)
	stopped, err := creator.Create(context.Background(), "dev", ModeClean)
	if err != nil {
		t.Fatal(err)
	}
	injected := errors.New("session directory sync failed after rename")
	originalSync := sessionSyncRoot
	defer func() { sessionSyncRoot = originalSync }()
	calls := 0
	sessionSyncRoot = func(directory *os.Root) error {
		calls++
		if calls <= 2 {
			return injected
		}
		return originalSync(directory)
	}
	service := NewDeleteService(configured, DeleteDependencies{
		Observer: backendFake, Deleter: backendFake,
		Gate: func(_ context.Context, root string, domainID domain.ID, expected Record, _ backend.Observer, reserve func() error) error {
			if root != configured.StateRoot || domainID != configured.ID || expected != stopped {
				t.Fatal("delete gate received wrong authority")
			}
			return reserve()
		},
		Finalize: func(context.Context, string, domain.ID, Record, backend.Observer) error { return injected },
	})
	for attempt := 1; attempt <= 2; attempt++ {
		if err := service.Delete(context.Background(), "dev"); !errors.Is(err, injected) {
			t.Fatalf("intent sync attempt %d = %v", attempt, err)
		}
		if len(backendFake.DeleteCalls()) != 0 {
			t.Fatalf("backend deleted without durable intent on attempt %d", attempt)
		}
	}
	current, err := LoadRecord(configured.StateRoot, "work", "dev")
	if err != nil || current.IntendedState != StateDeleting {
		t.Fatalf("visible deleting intent = %#v, %v", current, err)
	}
	if err := service.Delete(context.Background(), "dev"); !errors.Is(err, injected) || len(backendFake.DeleteCalls()) != 1 || calls != 3 {
		t.Fatalf("durable retry = %v, deletes %v, sync calls %d", err, backendFake.DeleteCalls(), calls)
	}
}
