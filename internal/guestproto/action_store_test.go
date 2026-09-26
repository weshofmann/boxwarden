package guestproto

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func actionStoreFixture(t *testing.T) (*Bootstrapper, ActionRequest, string) {
	t.Helper()
	b, _ := testBootstrapper(t)
	if _, err := b.Serial(context.Background(), testRequest()); err != nil {
		t.Fatal(err)
	}
	base := filepath.Join(b.Root, "var/lib/boxwarden")
	if err := os.MkdirAll(base, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(base, 0o700); err != nil {
		t.Fatal(err)
	}
	request := testActionRequest()
	request.Association = testRequest().Association
	request.Generation = testGeneration
	return b, request, base
}

func TestActionClaimPreventsInterruptedReplayAndReturnsExactReceipt(t *testing.T) {
	b, request, base := actionStoreFixture(t)
	if receipt, err := b.ClaimAction(request); err != nil || receipt != nil {
		t.Fatalf("first claim = %#v, %v", receipt, err)
	}
	claim := filepath.Join(base, "action-attempts", request.SessionID, request.AttemptID+".claim")
	if info, err := os.Lstat(claim); err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		t.Fatalf("durable claim = %v, %v", info, err)
	}
	if receipt, err := b.ClaimAction(request); receipt != nil || !errors.Is(err, ErrActionIndeterminate) {
		t.Fatalf("interrupted claim was replayable: %#v, %v", receipt, err)
	}
	changed := request
	changed.Argv = []string{"/usr/bin/false"}
	if _, err := b.ClaimAction(changed); err == nil {
		t.Fatal("changed argv reused an existing attempt")
	}
	_, digest, err := EncodeActionRequest(request)
	if err != nil {
		t.Fatal(err)
	}
	receipt := ActionReceipt{Version: Version, Association: request.Association,
		Generation: request.Generation, RecipeDigest: request.RecipeDigest,
		ActionID: request.ActionID, ActionPhase: request.ActionPhase,
		AttemptID: request.AttemptID, RequestSHA256: digest, State: "succeeded"}
	if err := b.PublishActionSuccess(request, receipt); err != nil {
		t.Fatal(err)
	}
	if got, err := b.ClaimAction(request); err != nil || got == nil || *got != receipt {
		t.Fatalf("completed replay = %#v, %v", got, err)
	}
	if err := b.PublishActionSuccess(request, receipt); err != nil {
		t.Fatalf("exact success retry = %v", err)
	}
}

func TestActionClaimRejectsForeignBindingAndMissingClaimSuccess(t *testing.T) {
	b, request, base := actionStoreFixture(t)
	foreign := request
	foreign.BackendObject = "other-system"
	if _, err := b.ClaimAction(foreign); err == nil {
		t.Fatal("foreign association claimed a guest action")
	}
	if _, err := os.Lstat(filepath.Join(base, "action-attempts")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("foreign request mutated marker store: %v", err)
	}
	_, digest, err := EncodeActionRequest(request)
	if err != nil {
		t.Fatal(err)
	}
	receipt := ActionReceipt{Version: Version, Association: request.Association,
		Generation: request.Generation, RecipeDigest: request.RecipeDigest,
		ActionID: request.ActionID, ActionPhase: request.ActionPhase,
		AttemptID: request.AttemptID, RequestSHA256: digest, State: "succeeded"}
	if err := b.PublishActionSuccess(request, receipt); err == nil {
		t.Fatal("success without a prior durable claim")
	}
}

func TestActionClaimRejectsCorruptAndLinkedClaim(t *testing.T) {
	b, request, base := actionStoreFixture(t)
	if _, err := b.ClaimAction(request); err != nil {
		t.Fatal(err)
	}
	claim := filepath.Join(base, "action-attempts", request.SessionID, request.AttemptID+".claim")
	if err := os.WriteFile(claim, []byte(`{"version":1,"request_sha256":"`+strings.Repeat("b", 64)+`"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := b.ClaimAction(request); err == nil {
		t.Fatal("changed durable claim accepted")
	}
	if err := os.Remove(claim); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(base, "outside"), claim); err != nil {
		t.Fatal(err)
	}
	if _, err := b.ClaimAction(request); err == nil {
		t.Fatal("linked durable claim accepted")
	}
}
