package projectx

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/weshofmann/boxwarden/internal/domain"
	"github.com/weshofmann/boxwarden/internal/session"
)

// Replacement retains the exact old bookmark and the journaled candidate.
// It is an advisory locator receipt; the caller must independently admit the
// session rebuild journal and runtime before completing this publication.
type Replacement struct {
	Version       int    `json:"version"`
	Before        Record `json:"before"`
	OperationID   string `json:"operation_id"`
	BackendObject string `json:"backend_object"`
	Base          string `json:"base"`
	IntentDigest  string `json:"intent_digest,omitempty"`
}

// MarshalJSON preserves the legacy shape while version 2 writes an explicit
// candidate digest even when empty: removing a recipe is a frozen choice, not
// a missing field that may default during recovery.
func (r Replacement) MarshalJSON() ([]byte, error) {
	type document Replacement
	if r.Version == 1 {
		return json.Marshal(document(r))
	}
	return json.Marshal(struct {
		document
		IntentDigest string `json:"intent_digest"`
	}{document: document(r), IntentDigest: r.IntentDigest})
}

// Witness reconstructs the frozen operation tuple, not the runtime pin witness.
// The admitted rebuild driver reads and validates its own original pin record.
func (r Replacement) Witness() session.RebuildJournal {
	return session.RebuildJournal{Version: 1, Domain: r.Before.Domain, SessionName: r.Before.Name, SessionID: r.Before.SessionID, OperationID: r.OperationID, Phase: session.RebuildCloned, OldBackend: r.Before.BackendObject, OldRevision: r.Before.Base, CandidateBackend: r.BackendObject, CandidateRevision: r.Base, OldIntentDigest: r.Before.RecipeIntentDigest, CandidateIntentDigest: r.IntentDigest}
}

func (r Replacement) Next() Record {
	next := r.Before
	next.Base = r.Base
	next.BackendObject = r.BackendObject
	next.RecipeIntentDigest = r.IntentDigest
	next.Version = 1
	if r.IntentDigest != "" {
		next.Version = 2
	}
	return next
}
func replacementName(name string) string { return ".replacement-" + name + ".json" }
func replacementHistory(r Replacement) string {
	return ".replacement-history-" + r.OperationID + ".json"
}
func validateReplacement(d domain.ID, r Replacement) error {
	if err := validateRecord(d, r.Before); err != nil {
		return err
	}
	if (r.Version != 1 && r.Version != 2) || !r.Before.Initialized || !validUUID(r.OperationID) || r.BackendObject != "boxwarden-"+string(d)+"-"+strings.ReplaceAll(r.OperationID, "-", "") || r.BackendObject == r.Before.BackendObject {
		return fmt.Errorf("invalid project replacement identity")
	}
	if r.Version == 1 && (r.Before.RecipeIntentDigest != "" || r.IntentDigest != "") ||
		r.Version == 2 && r.Before.RecipeIntentDigest == "" && r.IntentDigest == "" {
		return fmt.Errorf("project replacement version differs from recipe binding")
	}
	return validateRecord(d, r.Next())
}
func decodeReplacement(raw []byte, d domain.ID, name string) (Replacement, error) {
	var r Replacement
	if err := decodeVersionedDocument(raw, &r, []string{"version", "before", "operation_id", "backend_object", "base"}, []string{"intent_digest"}); err != nil {
		return r, err
	}
	var nested struct {
		Before json.RawMessage `json:"before"`
	}
	if err := json.Unmarshal(raw, &nested); err != nil {
		return r, err
	}
	if err := decodeRecord(nested.Before, &r.Before); err != nil {
		return r, err
	}
	if err := validateReplacement(d, r); err != nil {
		return r, err
	}
	if name != "" && r.Before.Name != name {
		return r, fmt.Errorf("replacement project name differs from file key")
	}
	return r, nil
}
func LoadReplacement(root string, d domain.ID, name string) (Replacement, error) {
	if err := validateKey(d, name); err != nil {
		return Replacement{}, err
	}
	dir, err := openProjects(root, false)
	if err != nil {
		return Replacement{}, err
	}
	defer dir.Close()
	raw, _, err := readDocument(dir, replacementName(name))
	if err != nil {
		return Replacement{}, err
	}
	return decodeReplacement(raw, d, name)
}

// LoadCompletedReplacement finds an immutable completed receipt for the exact
// current bookmark. History remains advisory: the caller must independently
// admit the candidate system before settling or using it. This read never
// creates state, and an older receipt cannot authorize a later bookmark.
func LoadCompletedReplacement(root string, d domain.ID, name string) (Replacement, error) {
	if err := validateKey(d, name); err != nil {
		return Replacement{}, err
	}
	dir, err := openProjects(root, false)
	if err != nil {
		return Replacement{}, err
	}
	defer dir.Close()
	if _, _, err := readDocument(dir, replacementName(name)); err == nil {
		return Replacement{}, fmt.Errorf("project replacement is still pending")
	} else if !errors.Is(err, os.ErrNotExist) {
		return Replacement{}, err
	}
	current, _, err := loadRecord(dir, d, name)
	if err != nil {
		return Replacement{}, err
	}
	f, err := dir.Open(".")
	if err != nil {
		return Replacement{}, err
	}
	entries, readErr := f.ReadDir(1025)
	closeErr := f.Close()
	if errors.Is(readErr, io.EOF) {
		readErr = nil
	}
	if err := errors.Join(readErr, closeErr); err != nil {
		return Replacement{}, err
	}
	if len(entries) > 1024 {
		return Replacement{}, fmt.Errorf("replacement history registry exceeds 1024 entries")
	}
	var matched Replacement
	for _, entry := range entries {
		key := entry.Name()
		if !strings.HasPrefix(key, ".replacement-history-") || strings.Contains(key, ".json.tmp-") {
			continue
		}
		id := strings.TrimSuffix(strings.TrimPrefix(key, ".replacement-history-"), ".json")
		if !strings.HasSuffix(key, ".json") || !validUUID(id) {
			return Replacement{}, fmt.Errorf("invalid replacement history key %q", key)
		}
		raw, _, err := readDocument(dir, key)
		if err != nil {
			return Replacement{}, err
		}
		// History is keyed by operation UUID, so decode the validated project name
		// from the receipt and bind the operation to the exact filename below.
		receipt, err := decodeReplacement(raw, d, "")
		if err != nil {
			return Replacement{}, err
		}
		if key != replacementHistory(receipt) {
			return Replacement{}, fmt.Errorf("replacement history operation differs from file key")
		}
		if receipt.Before.Name == name && receipt.Next() == current {
			matched = receipt
		}
	}
	if matched.Version == 0 {
		return Replacement{}, os.ErrNotExist
	}
	return matched, nil
}

// settleCompletedReplacement is used only after the pending intent is absent.
// Exact canonical receipt bytes and the exact final bookmark establish which
// publication's final unlink needs a directory sync; no bookmark is rewritten.
func settleCompletedReplacement(dir *os.Root, d domain.ID, intent Replacement) (Record, error) {
	current, _, err := loadRecord(dir, d, intent.Before.Name)
	if err != nil {
		return Record{}, err
	}
	next := intent.Next()
	if current != next {
		return Record{}, fmt.Errorf("completed replacement does not match current project binding")
	}
	historical, _, err := readDocument(dir, replacementHistory(intent))
	if err != nil {
		return Record{}, err
	}
	expected, err := json.Marshal(intent)
	if err != nil {
		return Record{}, err
	}
	expected = append(expected, '\n')
	if !bytes.Equal(historical, expected) {
		return Record{}, fmt.Errorf("replacement history differs from exact completed intent")
	}
	return next, syncProjectDirectory(dir)
}

// BeginReplacement snapshots only an exact old bookmark and a stopped cloned
// candidate from the existing rebuild journal. Caller holds the project lock.
func BeginReplacement(root string, d domain.ID, before Record, j session.RebuildJournal) (Replacement, error) {
	intent := Replacement{Version: 1, Before: before, OperationID: j.OperationID, BackendObject: j.CandidateBackend, Base: j.CandidateRevision, IntentDigest: j.CandidateIntentDigest}
	if before.RecipeIntentDigest != "" || j.CandidateIntentDigest != "" {
		intent.Version = 2
	}
	if err := validateReplacement(d, intent); err != nil {
		return Replacement{}, err
	}
	if j.Version != 1 || j.Domain != d || j.SessionName != before.Name || j.SessionID != before.SessionID || j.OldBackend != before.BackendObject || j.OldRevision != before.Base || j.Phase != session.RebuildCloned || j.OldIntentDigest != before.RecipeIntentDigest {
		return Replacement{}, fmt.Errorf("replacement witness differs from exact cloned project system")
	}
	dir, err := openProjects(root, false)
	if err != nil {
		return Replacement{}, err
	}
	defer dir.Close()
	current, _, err := loadRecord(dir, d, before.Name)
	if err != nil {
		return Replacement{}, err
	}
	if current != before {
		return Replacement{}, fmt.Errorf("project changed before replacement intent")
	}
	raw, _, err := readDocument(dir, replacementName(before.Name))
	if err == nil {
		prior, err := decodeReplacement(raw, d, before.Name)
		if err != nil {
			return Replacement{}, err
		}
		if prior != intent {
			return Replacement{}, fmt.Errorf("project replacement is already bound to another candidate")
		}
		return intent, syncProjectDirectory(dir)
	}
	if !errors.Is(err, os.ErrNotExist) {
		return Replacement{}, err
	}
	return intent, publish(dir, replacementName(before.Name), intent, nil)
}

// CompleteReplacement preserves a private immutable history receipt before
// changing only the system/base/recipe bookmark, then clears the retry intent last.
// Runtime completion is independently checked by the caller. A visible rename
// followed by failed sync is settled by an exact retry, never by another clone.
func CompleteReplacement(root string, d domain.ID, intent Replacement) (Record, error) {
	if err := validateReplacement(d, intent); err != nil {
		return Record{}, err
	}
	dir, err := openProjects(root, false)
	if err != nil {
		return Record{}, err
	}
	defer dir.Close()
	raw, _, err := readDocument(dir, replacementName(intent.Before.Name))
	if errors.Is(err, os.ErrNotExist) {
		return settleCompletedReplacement(dir, d, intent)
	}
	if err != nil {
		return Record{}, err
	}
	pending, err := decodeReplacement(raw, d, intent.Before.Name)
	if err != nil {
		return Record{}, err
	}
	if pending != intent {
		return Record{}, fmt.Errorf("replacement intent changed before completion")
	}
	current, info, err := loadRecord(dir, d, intent.Before.Name)
	if err != nil {
		return Record{}, err
	}
	next := intent.Next()
	if current != intent.Before && current != next {
		return Record{}, fmt.Errorf("project binding changed before replacement completion")
	}
	history := replacementHistory(intent)
	historical, _, historyErr := readDocument(dir, history)
	if historyErr == nil {
		if !bytes.Equal(historical, raw) {
			return Record{}, fmt.Errorf("replacement history differs from exact intent")
		}
		if err := syncProjectDirectory(dir); err != nil {
			return Record{}, err
		}
	} else if errors.Is(historyErr, os.ErrNotExist) {
		if err := publish(dir, history, intent, nil); err != nil {
			return Record{}, err
		}
	} else {
		return Record{}, historyErr
	}
	if err := publish(dir, next.Name+".json", next, info); err != nil {
		return Record{}, err
	}
	if err := dir.Remove(replacementName(next.Name)); err != nil {
		return Record{}, err
	}
	return next, syncProjectDirectory(dir)
}
