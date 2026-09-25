package session

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/weshofmann/boxwarden/internal/domain"
	"github.com/weshofmann/boxwarden/internal/recipe"
)

func actionAttemptFixture(t *testing.T) (string, ActionAttempt) {
	t.Helper()
	root := sessionRoot(t)
	value := recipe.Recipe{
		Version: 1,
		Source:  recipe.Source{Kind: "ubuntu-24.04.4-desktop-arm64", SHA256: "c2610520bf582976839a1724c669e1cfed0547427be5a0ad12d457b92b46ffbe"},
		Machine: recipe.Machine{CPUs: 4, MemoryMiB: 4096, SystemDiskGiB: 30},
		Steps:   []recipe.Step{{ID: "configure-agent", Phase: "once", Argv: []string{"/usr/bin/true"}}},
	}
	digest, err := PublishRecipeIntent(root, value)
	if err != nil {
		t.Fatal(err)
	}
	record := Record{
		Version: recordVersion, Domain: domain.ID("work"), Name: Name("dev"),
		ID: "13b0bf73-3bd5-4f1c-8bdc-71d50c36d6d0", Mode: ModeClean,
		IntendedState:  StateRunning,
		Backend:        BackendRef{Kind: "tart", ObjectID: "boxwarden-work-dev"},
		GoldenRevision: "golden-r1", RecipeIntentDigest: digest,
		StartGeneration: "00000000-0000-4000-8000-000000000003",
		Readiness:       ReadinessRecord{Status: ReadinessReady},
	}
	if err := SaveRecord(root, record.Domain, record); err != nil {
		t.Fatal(err)
	}
	return root, ActionAttempt{
		Version: 1, Domain: record.Domain, SessionName: string(record.Name),
		SessionID: record.ID, BackendObject: record.Backend.ObjectID,
		Generation: record.StartGeneration, RecipeDigest: digest,
		ActionID: "configure-agent", ActionPhase: "once",
		AttemptID: "00112233-4455-4677-8899-aabbccddeeff",
		State:     ActionAttemptReserved,
	}
}

func TestActionAttemptReserveAndExactTerminalAdvance(t *testing.T) {
	root, attempt := actionAttemptFixture(t)
	if err := ReserveActionAttempt(root, attempt); err != nil {
		t.Fatal(err)
	}
	if err := ReserveActionAttempt(root, attempt); err == nil {
		t.Fatal("duplicate attempt reservation accepted")
	}
	otherID := attempt
	otherID.AttemptID = "00112233-4455-4677-8899-aabbccddeeee"
	if err := ReserveActionAttempt(root, otherID); err == nil {
		t.Fatal("same once action reserved again under another attempt ID")
	}
	got, err := LoadActionAttempt(root, attempt.Domain, attempt.SessionID, attempt.AttemptID)
	if err != nil || got != attempt {
		t.Fatalf("loaded attempt = %#v, %v", got, err)
	}
	path := filepath.Join(root, "action-attempts", attempt.SessionID, attempt.AttemptID+".json")
	if info, err := os.Lstat(path); err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		t.Fatalf("attempt file = %v, %v", info, err)
	}
	skipped := attempt
	skipped.State = ActionAttemptSucceeded
	if err := advanceActionAttempt(root, attempt, skipped); err == nil {
		t.Fatal("success without a receipt digest accepted")
	}
	next := attempt
	next.State = ActionAttemptSucceeded
	next.ReceiptSHA256 = strings.Repeat("a", 64)
	if err := advanceActionAttempt(root, attempt, next); err != nil {
		t.Fatal(err)
	}
	if err := advanceActionAttempt(root, attempt, next); err != nil {
		t.Fatalf("idempotent post-rename retry: %v", err)
	}
	if got, err := LoadActionAttempt(root, attempt.Domain, attempt.SessionID, attempt.AttemptID); err != nil || got != next {
		t.Fatalf("terminal attempt = %#v, %v", got, err)
	}
	changed := next
	changed.State = ActionAttemptFailed
	changed.ReceiptSHA256 = ""
	if err := advanceActionAttempt(root, next, changed); err == nil {
		t.Fatal("terminal outcome rewritten")
	}
}

func TestActionAttemptRejectsFalseBindingAndUnsafeStorage(t *testing.T) {
	root, attempt := actionAttemptFixture(t)
	for label, mutate := range map[string]func(*ActionAttempt){
		"wrong recipe":     func(a *ActionAttempt) { a.RecipeDigest = strings.Repeat("b", 64) },
		"wrong action":     func(a *ActionAttempt) { a.ActionID = "other" },
		"wrong phase":      func(a *ActionAttempt) { a.ActionPhase = "startup" },
		"wrong generation": func(a *ActionAttempt) { a.Generation = "00000000-0000-4000-8000-000000000004" },
		"wrong backend":    func(a *ActionAttempt) { a.BackendObject = "other-backend" },
	} {
		t.Run(label, func(t *testing.T) {
			bad := attempt
			mutate(&bad)
			if err := ReserveActionAttempt(root, bad); err == nil {
				t.Fatal("false binding reserved")
			}
		})
	}
	if err := ReserveActionAttempt(root, attempt); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadActionAttempt(root, domain.ID("personal"), attempt.SessionID, attempt.AttemptID); err == nil {
		t.Fatal("foreign domain loaded attempt")
	}
	path := filepath.Join(root, "action-attempts", attempt.SessionID, attempt.AttemptID+".json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(raw, &object); err != nil {
		t.Fatal(err)
	}
	object["action_id"] = "other"
	raw, err = json.Marshal(object)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadActionAttempt(root, attempt.Domain, attempt.SessionID, attempt.AttemptID); err == nil {
		t.Fatal("corrupt action binding loaded")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "outside"), path); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadActionAttempt(root, attempt.Domain, attempt.SessionID, attempt.AttemptID); err == nil || errors.Is(err, os.ErrNotExist) {
		t.Fatalf("symlink attempt read = %v", err)
	}
}

func TestActionAttemptSuccessRejectsChangedRunningGeneration(t *testing.T) {
	root, attempt := actionAttemptFixture(t)
	if err := ReserveActionAttempt(root, attempt); err != nil {
		t.Fatal(err)
	}
	record, err := LoadRecord(root, string(attempt.Domain), attempt.SessionName)
	if err != nil {
		t.Fatal(err)
	}
	record.StartGeneration = "00000000-0000-4000-8000-000000000004"
	if err := SaveRecord(root, record.Domain, record); err != nil {
		t.Fatal(err)
	}
	next := attempt
	next.State = ActionAttemptSucceeded
	next.ReceiptSHA256 = strings.Repeat("a", 64)
	if err := advanceActionAttempt(root, attempt, next); err == nil {
		t.Fatal("stale generation accepted a success receipt")
	}
	if got, err := LoadActionAttempt(root, attempt.Domain, attempt.SessionID, attempt.AttemptID); err != nil || got != attempt {
		t.Fatalf("stale success changed journal = %#v, %v", got, err)
	}
}
