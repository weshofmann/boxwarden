package projectx

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRecipeReplacementFreezesBothIntentsAndPreservesWork(t *testing.T) {
	for _, withOldRecipe := range []bool{false, true} {
		t.Run(map[bool]string{false: "legacy upgrade", true: "recipe replacement"}[withOldRecipe], func(t *testing.T) {
			root, before, j := replacementFixture(t)
			if withOldRecipe {
				if err := json.Unmarshal(recipeRecordJSON(t, before, recipeDigestA), &before); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(recordPath(root), append(mustJSON(t, before), '\n'), 0600); err != nil {
					t.Fatal(err)
				}
				j.OldIntentDigest = recipeDigestA
			}
			j.CandidateIntentDigest = recipeDigestB
			j.CandidateRevision = before.Base // actions can change while the prepared base is reused.
			intent, err := BeginReplacement(root, "work", before, j)
			if err != nil {
				t.Fatal(err)
			}
			witness := intent.Witness()
			if witness.OldIntentDigest != j.OldIntentDigest || witness.CandidateIntentDigest != recipeDigestB || witness.OperationID != j.OperationID {
				t.Fatalf("intent lost in frozen witness: %+v", witness)
			}
			if intent.Version != 2 {
				t.Fatal("recipe replacement not versioned")
			}
			if got, err := LoadReplacement(root, "work", before.Name); err != nil || got != intent {
				t.Fatalf("retained intent %+v %v", got, err)
			}
			changed := j
			changed.CandidateIntentDigest = recipeDigestA
			if _, err := BeginReplacement(root, "work", before, changed); err == nil {
				t.Fatal("candidate recipe retargeted during retry")
			}
			changed = j
			changed.OldIntentDigest = recipeDigestB
			if _, err := BeginReplacement(root, "work", before, changed); err == nil {
				t.Fatal("foreign old intent admitted")
			}
			if _, err := CompleteReplacement(root, "work", intent); err != nil {
				t.Fatal(err)
			}
			got, err := Load(root, "work", before.Name)
			if err != nil {
				t.Fatal(err)
			}
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(mustJSON(t, got), &fields); err != nil {
				t.Fatal(err)
			}
			if got.Version != 2 || string(fields["recipe_intent_digest"]) != `"`+recipeDigestB+`"` {
				t.Fatalf("candidate recipe lost: %+v", got)
			}
			if got.Domain != before.Domain || got.Name != before.Name || got.SessionID != before.SessionID || got.VolumeID != before.VolumeID || got.FilesystemUUID != before.FilesystemUUID || got.SizeBytes != before.SizeBytes || got.ImportID != before.ImportID || got.ImportSource != before.ImportSource || got.Imported != before.Imported || got.Initialized != before.Initialized {
				t.Fatal("recipe replacement changed independent work identity")
			}
			if receipt, err := LoadCompletedReplacement(root, "work", before.Name); err != nil || receipt != intent {
				t.Fatalf("completed recipe receipt %+v %v", receipt, err)
			}
			if _, err := CompleteReplacement(root, "work", intent); err != nil {
				t.Fatalf("completed retry: %v", err)
			}
			if listed, err := List(root, "work"); err != nil || len(listed) != 1 || listed[0] != got {
				t.Fatalf("recipe history listing %+v %v", listed, err)
			}
		})
	}
}

func TestRecipeReplacementRejectsMissingHiddenAndMalformedIntent(t *testing.T) {
	root, before, j := replacementFixture(t)
	j.CandidateIntentDigest = recipeDigestB
	intent, err := BeginReplacement(root, "work", before, j)
	if err != nil {
		t.Fatal(err)
	}
	valid := string(mustJSON(t, intent))
	for name, raw := range map[string]string{
		"missing":       strings.Replace(valid, `,"intent_digest":"`+recipeDigestB+`"`, "", 1),
		"legacy hidden": strings.Replace(valid, `"version":2`, `"version":1`, 1),
		"null":          strings.Replace(valid, `"intent_digest":"`+recipeDigestB+`"`, `"intent_digest":null`, 1),
		"malformed":     strings.Replace(valid, recipeDigestB, "foreign", 1),
		"nested hidden": strings.Replace(valid, `"imported":true`, `"imported":true,"recipe_intent_digest":""`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(root, "projects", replacementName(before.Name))
			if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadReplacement(root, "work", before.Name); err == nil {
				t.Fatal("invalid replacement admitted")
			}
			if _, err := CompleteReplacement(root, "work", intent); err == nil {
				t.Fatal("invalid pending intent completed")
			}
			if got, err := Load(root, "work", before.Name); err != nil || got != before {
				t.Fatal("rejected pending intent changed work binding")
			}
		})
	}
}

func TestRecipeReplacementFinalSyncRetryKeepsExactHistory(t *testing.T) {
	root, before, j := replacementFixture(t)
	j.CandidateIntentDigest = recipeDigestB
	intent, err := BeginReplacement(root, "work", before, j)
	if err != nil {
		t.Fatal(err)
	}
	original := syncProjectDirectory
	t.Cleanup(func() { syncProjectDirectory = original })
	failed := false
	syncProjectDirectory = func(dir *os.Root) error {
		if _, err := dir.Lstat(replacementName(before.Name)); errors.Is(err, os.ErrNotExist) && !failed {
			failed = true
			return errors.New("synthetic final unlink sync failure")
		}
		return original(dir)
	}
	if _, err := CompleteReplacement(root, "work", intent); err == nil || !failed {
		t.Fatalf("sync failure hidden: %v", err)
	}
	current, err := os.ReadFile(recordPath(root))
	if err != nil {
		t.Fatal(err)
	}
	historyPath := filepath.Join(root, "projects", replacementHistory(intent))
	history, err := os.ReadFile(historyPath)
	if err != nil {
		t.Fatal(err)
	}
	retained, err := LoadCompletedReplacement(root, "work", before.Name)
	if err != nil || retained != intent {
		t.Fatalf("cannot recover recipe receipt %+v %v", retained, err)
	}
	if _, err := CompleteReplacement(root, "work", retained); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(recordPath(root))
	if err != nil || !bytes.Equal(current, after) {
		t.Fatal("retry rewrote current binding")
	}
	after, err = os.ReadFile(historyPath)
	if err != nil || !bytes.Equal(history, after) {
		t.Fatal("retry rewrote history")
	}
}

func TestRecipeReplacementExplicitRemovalIsFrozenAndLegacyShaped(t *testing.T) {
	root, before, j := replacementFixture(t)
	if err := json.Unmarshal(recipeRecordJSON(t, before, recipeDigestA), &before); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(recordPath(root), append(mustJSON(t, before), '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	j.OldIntentDigest = recipeDigestA
	intent, err := BeginReplacement(root, "work", before, j)
	if err != nil {
		t.Fatal(err)
	}
	if intent.Version != 2 || intent.Witness().OldIntentDigest != recipeDigestA || intent.Witness().CandidateIntentDigest != "" {
		t.Fatal("explicit removal not frozen")
	}
	raw, err := os.ReadFile(filepath.Join(root, "projects", replacementName(before.Name)))
	if err != nil || !bytes.Contains(raw, []byte(`"intent_digest":""`)) {
		t.Fatalf("removal receipt lacks explicit empty candidate: %s %v", raw, err)
	}
	if got, err := LoadReplacement(root, "work", before.Name); err != nil || got != intent {
		t.Fatalf("removal receipt cannot recover %+v %v", got, err)
	}
	changed := j
	changed.CandidateIntentDigest = recipeDigestB
	if _, err := BeginReplacement(root, "work", before, changed); err == nil {
		t.Fatal("removal retry adopted recipe")
	}
	next, err := CompleteReplacement(root, "work", intent)
	if err != nil || next.Version != 1 || bytes.Contains(mustJSON(t, next), []byte("recipe_intent_digest")) {
		t.Fatalf("removal failed %+v %v", next, err)
	}
	if receipt, err := LoadCompletedReplacement(root, "work", before.Name); err != nil || receipt != intent {
		t.Fatalf("removal history cannot recover %+v %v", receipt, err)
	}
}
