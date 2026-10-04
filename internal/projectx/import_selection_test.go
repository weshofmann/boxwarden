package projectx

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/weshofmann/boxwarden/internal/importx"
)

func TestFirstImportFreezesSelectionBeforeCaptureAndPreservesLegacySchemas(t *testing.T) {
	for _, version := range []int{1, 2} {
		t.Run(string(rune('0'+version)), func(t *testing.T) {
			root := privateRoot(t)
			before := fixtureRecord()
			before.Version = version
			if version == 2 {
				before.RecipeIntentDigest = strings.Repeat("a", 64)
			}
			if err := Create(root, "work", before); err != nil {
				t.Fatal(err)
			}
			legacy, err := os.ReadFile(recordPath(root))
			if err != nil || bytes.Contains(legacy, []byte("import_selection")) {
				t.Fatalf("legacy bytes changed: %s %v", legacy, err)
			}
			next := before
			next.Version = 3
			next.ImportID = testImportIDForSelection
			next.ImportSource = "/private/source"
			next.ImportSelection, err = importx.CanonicalSelection(importx.Selection{Excludes: []string{".git", "node_modules"}, ExpectedDigest: strings.Repeat("b", 64)})
			if err != nil {
				t.Fatal(err)
			}
			if err := Save(root, "work", next); err != nil {
				t.Fatal(err)
			}
			got, err := Load(root, "work", "dev")
			if err != nil || got != next {
				t.Fatalf("frozen selection: %#v %v", got, err)
			}
			raw, err := os.ReadFile(recordPath(root))
			if err != nil || !bytes.Contains(raw, []byte(`"recipe_intent_digest":`)) || !bytes.Contains(raw, []byte(`"import_selection":`)) {
				t.Fatalf("v3 required fields missing: %s %v", raw, err)
			}
			changed := got
			changed.ImportSelection, err = importx.CanonicalSelection(importx.Selection{Excludes: []string{".git"}})
			if err != nil {
				t.Fatal(err)
			}
			if err := Save(root, "work", changed); err == nil {
				t.Fatal("pending import selection broadened")
			}
			changed = got
			changed.Version = before.Version
			changed.ImportSelection = ""
			if err := Save(root, "work", changed); err == nil {
				t.Fatal("selection version regressed")
			}
			if loaded, err := Load(root, "work", "dev"); err != nil || loaded != got {
				t.Fatalf("failed save changed record: %#v %v", loaded, err)
			}
		})
	}
}

const testImportIDForSelection = "22222222-3333-4444-8555-666666666666"

func TestLegacyPendingImportCannotAcquireDifferentSelection(t *testing.T) {
	root := privateRoot(t)
	r := fixtureRecord()
	r.ImportID = testImportIDForSelection
	r.ImportSource = "/private/source"
	if err := Create(root, "work", r); err != nil {
		t.Fatal(err)
	}
	next := r
	next.Version = 3
	next.ImportSelection, _ = importx.CanonicalSelection(importx.Selection{Excludes: []string{"dist"}})
	if err := Save(root, "work", next); err == nil {
		t.Fatal("legacy pending import acquired exclusions")
	}
}

func TestSelectionRecordRejectsAmbiguousV3AndDoesNotBroadenOtherDocuments(t *testing.T) {
	r := fixtureRecord()
	r.Version = 3
	r.ImportID = testImportIDForSelection
	r.ImportSource = "/private/source"
	r.ImportSelection, _ = importx.CanonicalSelection(importx.Selection{})
	raw := mustJSON(t, r)
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"recipe_intent_digest", "import_selection"} {
		changed := make(map[string]json.RawMessage, len(fields))
		for k, v := range fields {
			changed[k] = v
		}
		delete(changed, name)
		encoded, err := json.Marshal(changed)
		if err != nil {
			t.Fatal(err)
		}
		var got Record
		if err := decodeRecord(encoded, &got); err == nil {
			t.Fatalf("missing v3 %s accepted", name)
		}
	}
	r.ImportSelection = `{"excludes":["dist"]}`
	if err := validateRecord("work", r); err == nil {
		t.Fatal("ambiguous selection binding accepted")
	}
	setup := fixtureSetup()
	setup.Version = 3
	var gotSetup Setup
	if err := decodeSetup(mustJSON(t, setup), &gotSetup); err == nil {
		t.Fatal("setup unexpectedly accepts record version3")
	}
}

func TestReplacementNextKeepsFrozenSelectionSchema(t *testing.T) {
	// The replacement changes an existing complete system receipt.

	r := fixtureRecord()
	r.SessionID = r.VolumeID
	r.BackendObject = "original-system"
	r.Version = 3
	r.ImportID = testImportIDForSelection
	r.ImportSource = "/private/source"
	r.ImportSelection, _ = importx.CanonicalSelection(importx.Selection{Excludes: []string{".git"}})
	for _, digest := range []string{"", strings.Repeat("c", 64)} {
		replacement := Replacement{Before: r, Base: "replacement-base", BackendObject: "replacement-object", IntentDigest: digest}
		next := replacement.Next()
		if next.Version != 3 || next.ImportSelection != r.ImportSelection || next.ImportID != r.ImportID || next.RecipeIntentDigest != digest {
			t.Fatalf("rebuild lost selection: %#v", next)
		}
		if err := validateRecord("work", next); err != nil {
			t.Fatal(err)
		}
	}
}

func TestProjectRecordsAcceptLargerExportableWorkspaceAndRejectOverflow(t *testing.T) {
	for _, size := range []int64{2 << 30, 4 << 30} {
		root := privateRoot(t)
		r := fixtureRecord()
		r.SizeBytes = size
		if err := Create(root, "work", r); err != nil {
			t.Fatalf("supported size %d: %v", size, err)
		}
		if got, err := Load(root, "work", "dev"); err != nil || got.SizeBytes != size {
			t.Fatalf("size receipt: %#v %v", got, err)
		}
	}
	r := fixtureRecord()
	r.SizeBytes = (4 << 30) + 512
	if err := Create(privateRoot(t), "work", r); err == nil {
		t.Fatal("unexportable workspace admitted")
	}
}

func TestReplacementRoundtripKeepsV3SelectionAndDoesNotChangeReplacementSchema(t *testing.T) {
	root, before, witness := replacementFixture(t)
	before.Version = 3
	before.ImportSelection, _ = importx.CanonicalSelection(importx.Selection{Excludes: []string{".git"}, ExpectedDigest: strings.Repeat("b", 64)})
	// Create a separate v3 bookmark rather than upgrading an already pending
	// legacy import, which ordinary Save deliberately refuses.
	if err := os.WriteFile(recordPath(root), mustJSON(t, before), 0o600); err != nil {
		t.Fatal(err)
	}
	intent, err := BeginReplacement(root, "work", before, witness)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadReplacement(root, "work", before.Name)
	if err != nil || loaded != intent {
		t.Fatalf("v3 nested pending bookmark: %#v %v", loaded, err)
	}
	if _, err := CompleteReplacement(root, "work", loaded); err != nil {
		t.Fatal(err)
	}
	receipt, err := LoadCompletedReplacement(root, "work", before.Name)
	if err != nil || receipt.Before != before || receipt.Next().Version != 3 || receipt.Next().ImportSelection != before.ImportSelection {
		t.Fatalf("v3 completed history: %#v %v", receipt, err)
	}
	if got, err := Load(root, "work", before.Name); err != nil || got != intent.Next() {
		t.Fatalf("v3 replacement bookmark: %#v %v", got, err)
	}
}
