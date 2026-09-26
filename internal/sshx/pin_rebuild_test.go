package sshx

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

const testRebuildOperation = "7fb25db7-3cc1-4d92-a04c-b60fd05fa421"

func testOldPinDigest(t *testing.T, pin HostKeyPin) string {
	t.Helper()
	raw, err := json.Marshal(pin)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}

func TestRebuildPinTransitionReplacesOnlyExactOldWitnessAndRetries(t *testing.T) {
	root := privateRoot(t)
	work := testDomain(t, "work", root)
	store := NewPinStore(work)
	oldBinding := testBinding(t, work)
	candidateBinding := oldBinding
	candidateBinding.BackendObject = "boxwarden-work-candidate"
	old, err := store.Admit(context.Background(), oldBinding, ObservedHostKey{Algorithm: "ssh-ed25519", PublicKey: testPublicKey})
	if err != nil {
		t.Fatal(err)
	}
	digest := testOldPinDigest(t, old)
	observed := ObservedHostKey{Algorithm: "ssh-ed25519", PublicKey: changedPublicKey}
	if _, err := store.TransitionRebuild(context.Background(), testRebuildOperation, oldBinding, candidateBinding, true, "0000000000000000000000000000000000000000000000000000000000000000", observed); err == nil {
		t.Fatal("wrong old pin digest authorized replacement")
	}
	if loaded, err := store.Load(context.Background(), oldBinding); err != nil || loaded != old {
		t.Fatalf("rejected transition changed old pin: %#v, %v", loaded, err)
	}
	newPin, err := store.TransitionRebuild(context.Background(), testRebuildOperation, oldBinding, candidateBinding, true, digest, observed)
	if err != nil || newPin.BackendObject != candidateBinding.BackendObject || newPin.PublicKey != changedPublicKey {
		t.Fatalf("rebuild transition = %#v, %v", newPin, err)
	}
	if loaded, err := store.Load(context.Background(), candidateBinding); err != nil || loaded != newPin {
		t.Fatalf("durable candidate pin = %#v, %v", loaded, err)
	}
	if _, err := store.Load(context.Background(), oldBinding); err == nil {
		t.Fatal("old backend binding still accepted after transition")
	}
	if retried, err := store.TransitionRebuild(context.Background(), testRebuildOperation, oldBinding, candidateBinding, true, digest, observed); err != nil || retried != newPin {
		t.Fatalf("post-rename retry = %#v, %v", retried, err)
	}
	if _, err := store.TransitionRebuild(context.Background(), testRebuildOperation, oldBinding, candidateBinding, true, digest, ObservedHostKey{Algorithm: "ssh-ed25519", PublicKey: testPublicKey}); err == nil {
		t.Fatal("candidate pin changed on conflicting retry")
	}
}

func TestRebuildPinTransitionAdmitsAbsentOldPinAndRejectsForeignStage(t *testing.T) {
	root := privateRoot(t)
	work := testDomain(t, "work", root)
	store := NewPinStore(work)
	oldBinding := testBinding(t, work)
	candidateBinding := oldBinding
	candidateBinding.BackendObject = "boxwarden-work-candidate"
	observed := ObservedHostKey{Algorithm: "ssh-ed25519", PublicKey: testPublicKey}
	if _, err := store.TransitionRebuild(context.Background(), testRebuildOperation, oldBinding, candidateBinding, false, "", observed); err != nil {
		t.Fatalf("absent old pin transition: %v", err)
	}
	if _, err := store.TransitionRebuild(context.Background(), testRebuildOperation, oldBinding, candidateBinding, false, "", ObservedHostKey{Algorithm: "ssh-ed25519", PublicKey: changedPublicKey}); err == nil {
		t.Fatal("conflicting candidate key accepted after pin transition")
	}
	foreign := candidateBinding
	foreign.SessionID = "00000000-0000-4000-8000-000000000002"
	if _, err := store.TransitionRebuild(context.Background(), testRebuildOperation, oldBinding, foreign, false, "", observed); err == nil {
		t.Fatal("foreign session binding accepted")
	}
}

func TestRebuildPinTransitionRejectsConflictingCrashStage(t *testing.T) {
	root := privateRoot(t)
	work := testDomain(t, "work", root)
	store := NewPinStore(work)
	oldBinding := testBinding(t, work)
	candidateBinding := oldBinding
	candidateBinding.BackendObject = "boxwarden-work-candidate"
	old, err := store.Admit(context.Background(), oldBinding, ObservedHostKey{Algorithm: "ssh-ed25519", PublicKey: testPublicKey})
	if err != nil {
		t.Fatal(err)
	}
	stageDir := filepath.Join(root, "identity", rebuildPinStageDirectory)
	if err := os.Mkdir(stageDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stageDir, testRebuildOperation+".json"), []byte("foreign stage"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.TransitionRebuild(context.Background(), testRebuildOperation, oldBinding, candidateBinding, true, testOldPinDigest(t, old), ObservedHostKey{Algorithm: "ssh-ed25519", PublicKey: changedPublicKey}); err == nil {
		t.Fatal("conflicting staged pin replaced old pin")
	}
	if loaded, err := store.Load(context.Background(), oldBinding); err != nil || loaded != old {
		t.Fatalf("conflicting stage changed old pin: %#v, %v", loaded, err)
	}
}

func TestRebuildPinTransitionResumesExactCrashStage(t *testing.T) {
	root := privateRoot(t)
	work := testDomain(t, "work", root)
	store := NewPinStore(work)
	oldBinding := testBinding(t, work)
	candidateBinding := oldBinding
	candidateBinding.BackendObject = "boxwarden-work-candidate"
	old, err := store.Admit(context.Background(), oldBinding, ObservedHostKey{Algorithm: "ssh-ed25519", PublicKey: testPublicKey})
	if err != nil {
		t.Fatal(err)
	}
	public, _, fingerprint, err := parseEd25519PublicKey(changedPublicKey)
	if err != nil {
		t.Fatal(err)
	}
	want := HostKeyPin{Version: hostKeyPinVersion, Domain: work.ID, SessionID: oldBinding.SessionID, BackendKind: "tart",
		BackendObject: candidateBinding.BackendObject, Algorithm: "ssh-ed25519", PublicKey: public, Fingerprint: fingerprint}
	raw, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	stageDir := filepath.Join(root, "identity", rebuildPinStageDirectory)
	if err := os.Mkdir(stageDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stageDir, testRebuildOperation+".json"), append(raw, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := store.TransitionRebuild(context.Background(), testRebuildOperation, oldBinding, candidateBinding, true, testOldPinDigest(t, old), ObservedHostKey{Algorithm: "ssh-ed25519", PublicKey: changedPublicKey})
	if err != nil || got != want {
		t.Fatalf("exact crash-stage retry = %#v, %v", got, err)
	}
	if _, err := os.Lstat(filepath.Join(stageDir, testRebuildOperation+".json")); !os.IsNotExist(err) {
		t.Fatalf("moved crash stage still present: %v", err)
	}
}
