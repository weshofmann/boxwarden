package session

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"syscall"

	"github.com/weshofmann/boxwarden/internal/domain"
	"github.com/weshofmann/boxwarden/internal/recipe"
	"github.com/weshofmann/boxwarden/internal/renamex"
)

const maxActionAttemptBytes = 2048

type ActionAttemptState string

const (
	ActionAttemptReserved      ActionAttemptState = "reserved"
	ActionAttemptSucceeded     ActionAttemptState = "succeeded"
	ActionAttemptFailed        ActionAttemptState = "failed"
	ActionAttemptIndeterminate ActionAttemptState = "indeterminate"
)

// ActionAttempt is an operational record, not proof that guest software is
// trustworthy. A reserved attempt after interruption is indeterminate and
// must never cause an automatic replay of a once action. The receipt digest
// binds a separately checked guest receipt to one exact successful attempt.
type ActionAttempt struct {
	Version       int                `json:"version"`
	Domain        domain.ID          `json:"domain"`
	SessionName   string             `json:"session_name"`
	SessionID     string             `json:"session_id"`
	BackendObject string             `json:"backend_object"`
	Generation    string             `json:"generation"`
	RecipeDigest  string             `json:"recipe_digest"`
	ActionID      string             `json:"action_id"`
	ActionPhase   string             `json:"action_phase"`
	AttemptID     string             `json:"attempt_id"`
	State         ActionAttemptState `json:"state"`
	ReceiptSHA256 string             `json:"receipt_sha256"`
}

func validateActionAttempt(a ActionAttempt) error {
	parsedDomain, err := domain.Parse(string(a.Domain))
	if err != nil || parsedDomain != a.Domain {
		return fmt.Errorf("invalid action attempt domain")
	}
	name, err := ParseName(a.SessionName)
	if err != nil || string(name) != a.SessionName ||
		a.Version != 1 || !validUUID(a.SessionID) || !validUUID(a.Generation) ||
		!validUUID(a.AttemptID) || a.AttemptID == "00000000-0000-0000-0000-000000000000" ||
		!validBackendObjectID(a.BackendObject) || !lowerSHA256(a.RecipeDigest) ||
		!validActionID(a.ActionID) {
		return fmt.Errorf("invalid action attempt identity")
	}
	switch a.ActionPhase {
	case "once", "reconfigure", "startup", "launch":
	default:
		return fmt.Errorf("invalid action phase %q", a.ActionPhase)
	}
	switch a.State {
	case ActionAttemptReserved, ActionAttemptFailed, ActionAttemptIndeterminate:
		if a.ReceiptSHA256 != "" {
			return fmt.Errorf("unfinished or failed action cannot claim a receipt")
		}
	case ActionAttemptSucceeded:
		if !lowerSHA256(a.ReceiptSHA256) {
			return fmt.Errorf("successful action requires a receipt digest")
		}
	default:
		return fmt.Errorf("invalid action attempt state %q", a.State)
	}
	return nil
}

func validActionID(value string) bool {
	if len(value) < 1 || len(value) > 64 || value[0] < 'a' || value[0] > 'z' {
		return false
	}
	for _, c := range value[1:] {
		if c < 'a' || c > 'z' {
			if c < '0' || c > '9' {
				if c != '-' {
					return false
				}
			}
		}
	}
	return true
}

func admitActionIntent(stateRoot string, a ActionAttempt) error {
	raw, err := LoadRecipeIntent(stateRoot, a.RecipeDigest)
	if err != nil {
		return fmt.Errorf("load exact action recipe: %w", err)
	}
	intent, err := recipe.DecodeIntent(raw)
	if err != nil {
		return err
	}
	for _, step := range intent.Steps {
		if step.ID == a.ActionID && step.Phase == a.ActionPhase {
			return nil
		}
	}
	if a.ActionPhase == "launch" {
		for _, launch := range intent.Launch {
			if launch.ID == a.ActionID {
				return nil
			}
		}
	}
	return fmt.Errorf("action %q/%q is absent from exact recipe", a.ActionPhase, a.ActionID)
}

// ReserveActionAttempt durably records an exact running-generation operation
// before any guest action. The caller must hold the session operation lock and
// establish fresh READY; the stored readiness bit alone is not fresh evidence.
// This storage primitive does not execute an action or authorize replay.
func ReserveActionAttempt(stateRoot string, a ActionAttempt) error {
	if err := validateActionAttempt(a); err != nil {
		return err
	}
	if a.State != ActionAttemptReserved {
		return fmt.Errorf("new action attempt must begin reserved")
	}
	record, err := LoadRecord(stateRoot, string(a.Domain), a.SessionName)
	if err != nil {
		return err
	}
	if !actionMatchesRunningRecord(a, record) {
		return fmt.Errorf("action attempt differs from running session binding")
	}
	if err := admitActionIntent(stateRoot, a); err != nil {
		return err
	}
	root, err := openSessionStateRoot(stateRoot)
	if err != nil {
		return err
	}
	defer root.Close()
	attempts, err := openSessionChild(root, "action-attempts", true)
	if err != nil {
		return err
	}
	defer attempts.Close()
	sessionAttempts, err := openSessionChild(attempts, a.SessionID, true)
	if err != nil {
		return err
	}
	defer sessionAttempts.Close()
	if err := rejectActionReplay(stateRoot, sessionAttempts, a); err != nil {
		return err
	}
	raw, err := encodeActionAttempt(a)
	if err != nil {
		return err
	}
	target := a.AttemptID + ".json"
	temporaryName, err := sessionTemporaryName(target)
	if err != nil {
		return err
	}
	temporary, err := sessionAttempts.OpenFile(temporaryName, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return err
	}
	defer sessionAttempts.Remove(temporaryName)
	if _, err = temporary.Write(raw); err == nil {
		err = temporary.Sync()
	}
	err = errors.Join(err, temporary.Close())
	if err != nil {
		return err
	}
	if err := renamex.NoReplace(sessionAttempts, temporaryName, target); err != nil {
		return fmt.Errorf("publish action attempt: %w", err)
	}
	return sessionSyncRoot(sessionAttempts)
}

func actionMatchesRunningRecord(a ActionAttempt, record Record) bool {
	return record.Domain == a.Domain && string(record.Name) == a.SessionName &&
		record.ID == a.SessionID && record.Backend.Kind == "tart" &&
		record.Backend.ObjectID == a.BackendObject &&
		record.RecipeIntentDigest == a.RecipeDigest &&
		record.StartGeneration == a.Generation &&
		record.IntendedState == StateRunning &&
		record.Readiness.Status == ReadinessReady
}

// The session lock serializes this scan with reservations and terminal writes.
// A corrupt or unexplained registry entry blocks execution rather than
// allowing a second attempt to hide an uncertain first attempt.
func rejectActionReplay(stateRoot string, sessionAttempts *os.Root, next ActionAttempt) error {
	directory, err := sessionAttempts.Open(".")
	if err != nil {
		return err
	}
	entries, readErr := directory.ReadDir(4097)
	closeErr := directory.Close()
	if errors.Is(readErr, io.EOF) {
		readErr = nil
	}
	if err := errors.Join(readErr, closeErr); err != nil {
		return err
	}
	if len(entries) > 4096 {
		return fmt.Errorf("action attempt registry exceeds bound")
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			return fmt.Errorf("unexpected action attempt entry %q", entry.Name())
		}
		id := strings.TrimSuffix(entry.Name(), ".json")
		prior, err := LoadActionAttempt(stateRoot, next.Domain, next.SessionID, id)
		if err != nil {
			return fmt.Errorf("inspect prior action attempt %q: %w", id, err)
		}
		if prior.ActionID == next.ActionID && prior.ActionPhase == next.ActionPhase &&
			prior.RecipeDigest == next.RecipeDigest && prior.BackendObject == next.BackendObject &&
			(next.ActionPhase == "once" || prior.Generation == next.Generation) {
			return fmt.Errorf("action %q/%q already has an attempt on this system", next.ActionPhase, next.ActionID)
		}
	}
	return nil
}

// LoadActionAttempt reads one exact domain/session/attempt identity, including
// re-admission of the action in the immutable stored recipe. Historic attempts
// remain readable after a system rebuild, but do not authorize action replay.
func LoadActionAttempt(stateRoot string, expectedDomain domain.ID, sessionID, attemptID string) (ActionAttempt, error) {
	if _, err := domain.Parse(string(expectedDomain)); err != nil {
		return ActionAttempt{}, err
	}
	if !validUUID(sessionID) || !validUUID(attemptID) {
		return ActionAttempt{}, fmt.Errorf("invalid action attempt path")
	}
	root, err := openSessionStateRoot(stateRoot)
	if err != nil {
		return ActionAttempt{}, err
	}
	defer root.Close()
	attempts, err := openSessionChild(root, "action-attempts", false)
	if err != nil {
		return ActionAttempt{}, err
	}
	defer attempts.Close()
	sessionAttempts, err := openSessionChild(attempts, sessionID, false)
	if err != nil {
		return ActionAttempt{}, err
	}
	defer sessionAttempts.Close()
	file, err := openSessionPrivateRegular(sessionAttempts, attemptID+".json")
	if err != nil {
		return ActionAttempt{}, err
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, maxActionAttemptBytes+1))
	if err != nil || len(raw) > maxActionAttemptBytes {
		return ActionAttempt{}, fmt.Errorf("read bounded action attempt: %v", err)
	}
	a, err := decodeActionAttempt(raw)
	if err != nil {
		return ActionAttempt{}, err
	}
	if a.Domain != expectedDomain || a.SessionID != sessionID || a.AttemptID != attemptID {
		return ActionAttempt{}, fmt.Errorf("action attempt differs from requested binding")
	}
	if err := admitActionIntent(stateRoot, a); err != nil {
		return ActionAttempt{}, err
	}
	return a, nil
}

// advanceActionAttempt may record only one terminal result for a reserved
// attempt. A retry accepts an already visible exact result after uncertain
// directory sync. Success requires a digest of a separately checked receipt.
func advanceActionAttempt(stateRoot string, expected, next ActionAttempt) error {
	if err := validateActionAttempt(expected); err != nil {
		return err
	}
	if err := validateActionAttempt(next); err != nil {
		return err
	}
	if expected.State != ActionAttemptReserved || next.State == ActionAttemptReserved {
		return fmt.Errorf("action attempt cannot advance from or to this state")
	}
	want := expected
	want.State = next.State
	want.ReceiptSHA256 = next.ReceiptSHA256
	if next != want {
		return fmt.Errorf("action attempt transition changed identity")
	}
	current, err := LoadActionAttempt(stateRoot, expected.Domain, expected.SessionID, expected.AttemptID)
	if err != nil {
		return err
	}
	if current == next {
		return nil
	}
	if current != expected {
		return fmt.Errorf("action attempt changed before terminal result")
	}
	if next.State == ActionAttemptSucceeded {
		record, err := LoadRecord(stateRoot, string(next.Domain), next.SessionName)
		if err != nil {
			return err
		}
		if !actionMatchesRunningRecord(next, record) {
			return fmt.Errorf("successful action differs from running session binding")
		}
	}
	root, err := openSessionStateRoot(stateRoot)
	if err != nil {
		return err
	}
	defer root.Close()
	attempts, err := openSessionChild(root, "action-attempts", false)
	if err != nil {
		return err
	}
	defer attempts.Close()
	sessionAttempts, err := openSessionChild(attempts, expected.SessionID, false)
	if err != nil {
		return err
	}
	defer sessionAttempts.Close()
	target := expected.AttemptID + ".json"
	temporaryName, err := sessionTemporaryName(target)
	if err != nil {
		return err
	}
	temporary, err := sessionAttempts.OpenFile(temporaryName, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return err
	}
	defer sessionAttempts.Remove(temporaryName)
	raw, err := encodeActionAttempt(next)
	if err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err = temporary.Write(raw); err == nil {
		err = temporary.Sync()
	}
	err = errors.Join(err, temporary.Close())
	if err != nil {
		return err
	}
	if err := sessionAttempts.Rename(temporaryName, target); err != nil {
		return err
	}
	return sessionSyncRoot(sessionAttempts)
}

func encodeActionAttempt(a ActionAttempt) ([]byte, error) {
	raw, err := json.Marshal(a)
	if err != nil {
		return nil, err
	}
	raw = append(raw, '\n')
	if len(raw) > maxActionAttemptBytes {
		return nil, fmt.Errorf("action attempt exceeds bound")
	}
	return raw, nil
}

func decodeActionAttempt(raw []byte) (ActionAttempt, error) {
	var a ActionAttempt
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if err := requireObjectStart(decoder); err != nil {
		return a, err
	}
	seen := map[string]bool{}
	for decoder.More() {
		field, err := objectField(decoder, seen)
		if err != nil {
			return ActionAttempt{}, err
		}
		switch field {
		case "version":
			err = decoder.Decode(&a.Version)
		case "domain":
			err = decoder.Decode(&a.Domain)
		case "session_name":
			err = decoder.Decode(&a.SessionName)
		case "session_id":
			err = decoder.Decode(&a.SessionID)
		case "backend_object":
			err = decoder.Decode(&a.BackendObject)
		case "generation":
			err = decoder.Decode(&a.Generation)
		case "recipe_digest":
			err = decoder.Decode(&a.RecipeDigest)
		case "action_id":
			err = decoder.Decode(&a.ActionID)
		case "action_phase":
			err = decoder.Decode(&a.ActionPhase)
		case "attempt_id":
			err = decoder.Decode(&a.AttemptID)
		case "state":
			err = decoder.Decode(&a.State)
		case "receipt_sha256":
			err = decoder.Decode(&a.ReceiptSHA256)
		default:
			return ActionAttempt{}, fmt.Errorf("unknown action attempt field %q", field)
		}
		if err != nil {
			return ActionAttempt{}, fmt.Errorf("action attempt field %q: %w", field, err)
		}
	}
	if err := requireObjectEnd(decoder); err != nil {
		return ActionAttempt{}, err
	}
	if token, err := decoder.Token(); err != io.EOF {
		return ActionAttempt{}, fmt.Errorf("trailing action attempt token %v: %v", token, err)
	}
	for _, field := range []string{"version", "domain", "session_name", "session_id", "backend_object", "generation", "recipe_digest", "action_id", "action_phase", "attempt_id", "state", "receipt_sha256"} {
		if !seen[field] {
			return ActionAttempt{}, fmt.Errorf("missing action attempt field %q", field)
		}
	}
	if err := validateActionAttempt(a); err != nil {
		return ActionAttempt{}, err
	}
	return a, nil
}
