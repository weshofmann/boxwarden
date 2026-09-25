package guestproto

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

type fakeActionExecutor struct {
	calls [][]string
	err   error
}

func (f *fakeActionExecutor) Run(_ context.Context, argv []string) error {
	f.calls = append(f.calls, append([]string(nil), argv...))
	return f.err
}

func TestExecuteActionClaimsThenRunsOnceAndReturnsBoundReceipt(t *testing.T) {
	b, request, _ := actionStoreFixture(t)
	executor := &fakeActionExecutor{}
	b.ActionExecutor = executor
	receipt, err := b.ExecuteAction(context.Background(), request)
	if err != nil || receipt.State != "succeeded" {
		t.Fatalf("first action = %#v, %v", receipt, err)
	}
	if len(executor.calls) != 1 || !slices.Equal(executor.calls[0], request.Argv) {
		t.Fatalf("command boundaries changed: %#v", executor.calls)
	}
	if _, err := EncodeActionReceipt(request, receipt); err != nil {
		t.Fatalf("receipt does not bind request: %v", err)
	}
	repeated, err := b.ExecuteAction(context.Background(), request)
	if err != nil || repeated != receipt || len(executor.calls) != 1 {
		t.Fatalf("exact retry reran action or changed receipt: %#v, %v, calls=%d", repeated, err, len(executor.calls))
	}
}

func TestExecuteActionFailureLeavesIndeterminateClaimWithoutReplay(t *testing.T) {
	b, request, _ := actionStoreFixture(t)
	executor := &fakeActionExecutor{err: errors.New("guest command failed")}
	b.ActionExecutor = executor
	if _, err := b.ExecuteAction(context.Background(), request); err == nil {
		t.Fatal("failing command reported success")
	}
	if _, err := b.ExecuteAction(context.Background(), request); !errors.Is(err, ErrActionIndeterminate) {
		t.Fatalf("interrupted retry = %v", err)
	}
	if len(executor.calls) != 1 {
		t.Fatalf("failed command replayed: %d calls", len(executor.calls))
	}
}

func TestExecuteActionRejectsForeignBindingBeforeRunner(t *testing.T) {
	b, request, _ := actionStoreFixture(t)
	executor := &fakeActionExecutor{}
	b.ActionExecutor = executor
	request.BackendObject = "foreign-system"
	if _, err := b.ExecuteAction(context.Background(), request); err == nil || len(executor.calls) != 0 {
		t.Fatalf("foreign request reached runner: %v, calls=%d", err, len(executor.calls))
	}
}

func TestExecuteActionLeavesDesktopLaunchDisabled(t *testing.T) {
	b, request, base := actionStoreFixture(t)
	executor := &fakeActionExecutor{}
	b.ActionExecutor = executor
	request.ActionPhase = "launch"
	if _, err := b.ExecuteAction(context.Background(), request); err == nil || len(executor.calls) != 0 {
		t.Fatalf("desktop launch reached runner: %v, calls=%d", err, len(executor.calls))
	}
	if _, err := os.Lstat(filepath.Join(base, "action-attempts")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("disabled launch created claim store: %v", err)
	}
}
