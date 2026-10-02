package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/weshofmann/boxwarden/internal/backend"
	"github.com/weshofmann/boxwarden/internal/projectx"
	"github.com/weshofmann/boxwarden/internal/session"
)

func TestProjectAdmissionRejectsRecipeDigestMismatchReadOnly(t *testing.T) {
	for _, projectHasRecipe := range []bool{false, true} {
		t.Run(map[bool]string{false: "legacy unexpected intent", true: "recipe retargeted"}[projectHasRecipe], func(t *testing.T) {
			prefix, d, o, _, out := projectFixture(t)
			if err := Run(t.Context(), append(prefix, "project", "create", "demo"), o); err != nil {
				t.Fatal(err)
			}
			r, err := projectx.Load(d.StateRoot, d.ID, "demo")
			if err != nil {
				t.Fatal(err)
			}
			if projectHasRecipe {
				r.Version = 2
				raw, err := json.Marshal(r)
				if err != nil {
					t.Fatal(err)
				}
				raw = []byte(strings.TrimSuffix(string(raw), "}") + `,"recipe_intent_digest":"` + strings.Repeat("a", 64) + `"}`)
				if err := os.WriteFile(filepath.Join(d.StateRoot, "projects", "demo.json"), raw, 0600); err != nil {
					t.Fatal(err)
				}
				r, err = projectx.Load(d.StateRoot, d.ID, "demo")
				if err != nil {
					t.Fatal(err)
				}
			}
			s, err := session.LoadRecord(d.StateRoot, string(d.ID), "demo")
			if err != nil {
				t.Fatal(err)
			}
			s.RecipeIntentDigest = strings.Repeat("b", 64)
			if err := session.SaveRecord(d.StateRoot, d.ID, s); err != nil {
				t.Fatal(err)
			}
			before := snapshotProjectTree(t, d.StateRoot)
			if _, err := boundProjectSession(d, r); err == nil {
				t.Fatal("project adopted a different session recipe")
			}
			out.Reset()
			if err := Run(t.Context(), append(prefix, "project", "list"), o); err == nil || out.Len() != 0 {
				t.Fatalf("foreign recipe listed: %v %s", err, out)
			}
			if !reflect.DeepEqual(before, snapshotProjectTree(t, d.StateRoot)) {
				t.Fatal("binding admission wrote state")
			}
		})
	}
}

func TestProjectListPendingRecipeAcceptsOnlyExactSystemAndJournalIntents(t *testing.T) {
	prefix, d, o, b, out := projectFixture(t)
	if err := Run(t.Context(), append(prefix, "project", "create", "demo"), o); err != nil {
		t.Fatal(err)
	}
	before, err := projectx.Load(d.StateRoot, d.ID, "demo")
	if err != nil {
		t.Fatal(err)
	}
	j := session.RebuildJournal{Version: 1, Domain: d.ID, SessionName: before.Name, SessionID: before.SessionID, OperationID: "55552233-4455-6677-8899-aabbccddeeff", Phase: session.RebuildCloned, OldBackend: before.BackendObject, OldRevision: before.Base, CandidateBackend: "boxwarden-alpha-55552233445566778899aabbccddeeff", CandidateRevision: before.Base, CandidateIntentDigest: strings.Repeat("a", 64)}
	if _, err := projectx.BeginReplacement(d.StateRoot, d.ID, before, j); err != nil {
		t.Fatal(err)
	}
	s, err := session.LoadRecord(d.StateRoot, string(d.ID), before.Name)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(d.StateRoot, "rebuilds"), 0700); err != nil {
		t.Fatal(err)
	}
	saveJournal := func(j session.RebuildJournal) {
		t.Helper()
		raw, err := json.Marshal(j)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d.StateRoot, "rebuilds", "demo.json"), raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	saveJournal(j)
	check := func(wantError bool) {
		t.Helper()
		out.Reset()
		tree := snapshotProjectTree(t, d.StateRoot)
		err := Run(t.Context(), append(prefix, "project", "list"), o)
		if wantError {
			if err == nil || out.Len() != 0 {
				t.Fatalf("foreign intent listed: %v %s", err, out)
			}
		} else if err != nil || !strings.Contains(out.String(), "state: rebuild pending") {
			t.Fatalf("exact pending binding unavailable: %v %s", err, out)
		}
		if !reflect.DeepEqual(tree, snapshotProjectTree(t, d.StateRoot)) {
			t.Fatal("pending list changed durable state")
		}
	}
	check(false) // exact old system, no old recipe.
	s.RecipeIntentDigest = j.CandidateIntentDigest
	if err := session.SaveRecord(d.StateRoot, d.ID, s); err != nil {
		t.Fatal(err)
	}
	check(true) // candidate digest cannot authorize the old object.
	s.Backend.ObjectID = j.CandidateBackend
	if err := session.SaveRecord(d.StateRoot, d.ID, s); err != nil {
		t.Fatal(err)
	}
	b.SetObservation(backend.Observation{ObjectID: j.CandidateBackend, Exists: true, State: backend.ObjectStopped})
	check(false)
	s.RecipeIntentDigest = strings.Repeat("b", 64)
	if err := session.SaveRecord(d.StateRoot, d.ID, s); err != nil {
		t.Fatal(err)
	}
	check(true)
	s.RecipeIntentDigest = j.CandidateIntentDigest
	if err := session.SaveRecord(d.StateRoot, d.ID, s); err != nil {
		t.Fatal(err)
	}
	j.CandidateIntentDigest = strings.Repeat("b", 64)
	saveJournal(j)
	check(true) // journal must match the frozen candidate digest too.
	j.CandidateIntentDigest = strings.Repeat("a", 64)
	j.OldIntentDigest = strings.Repeat("b", 64)
	saveJournal(j)
	check(true)
}
