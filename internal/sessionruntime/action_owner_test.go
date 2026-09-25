package sessionruntime

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/weshofmann/boxwarden/internal/guestproto"
	"github.com/weshofmann/boxwarden/internal/recipe"
	"github.com/weshofmann/boxwarden/internal/session"
)

func TestOwnerActionAdmissionRechecksExactReservedRecipeStep(t *testing.T) {
	f := newFixture(t)
	intent := recipe.Recipe{
		Version: 1,
		Source:  recipe.Source{Kind: "ubuntu-24.04.4-desktop-arm64", SHA256: "c2610520bf582976839a1724c669e1cfed0547427be5a0ad12d457b92b46ffbe"},
		Machine: recipe.Machine{CPUs: 4, MemoryMiB: 4096, SystemDiskGiB: 30},
		Steps:   []recipe.Step{{ID: "configure-agent", Phase: "once", Argv: []string{"/usr/bin/true"}}},
	}
	digest, err := session.PublishRecipeIntent(f.root, intent)
	if err != nil {
		t.Fatal(err)
	}
	f.record.RecipeIntentDigest = digest
	f.record.IntendedState = session.StateRunning
	f.record.Readiness = session.ReadinessRecord{Status: session.ReadinessReady}
	if err := session.SaveRecord(f.root, f.record.Domain, f.record); err != nil {
		t.Fatal(err)
	}
	attemptID := "00112233-4455-4677-8899-aabbccddeeee"
	request := guestproto.ActionRequest{Version: guestproto.Version,
		Association: guestproto.Association{Domain: "work", SessionID: f.record.ID, BackendKind: "tart", BackendObject: f.record.Backend.ObjectID},
		Generation:  f.record.StartGeneration, RecipeDigest: digest, ActionID: "configure-agent", ActionPhase: "once", AttemptID: attemptID, Argv: []string{"/usr/bin/true"}}
	if err := admitOwnerAction(f.root, "dev", f.request.Binding, request); err == nil {
		t.Fatal("unreserved action admitted")
	}
	attempt := session.ActionAttempt{Version: 1, Domain: f.record.Domain, SessionName: "dev", SessionID: f.record.ID,
		BackendObject: f.record.Backend.ObjectID, Generation: f.record.StartGeneration, RecipeDigest: digest,
		ActionID: request.ActionID, ActionPhase: request.ActionPhase, AttemptID: attemptID, State: session.ActionAttemptReserved}
	if err := session.ReserveActionAttempt(f.root, attempt); err != nil {
		t.Fatal(err)
	}
	if err := admitOwnerAction(f.root, "dev", f.request.Binding, request); err != nil {
		t.Fatalf("exact reservation refused: %v", err)
	}
	for name, mutate := range map[string]func(*guestproto.ActionRequest){
		"changed argv":       func(r *guestproto.ActionRequest) { r.Argv = []string{"/usr/bin/false"} },
		"changed recipe":     func(r *guestproto.ActionRequest) { r.RecipeDigest = strings.Repeat("b", 64) },
		"changed generation": func(r *guestproto.ActionRequest) { r.Generation = "00000000-0000-4000-8000-000000000004" },
		"changed attempt":    func(r *guestproto.ActionRequest) { r.AttemptID = "00000000-0000-4000-8000-000000000005" },
	} {
		t.Run(name, func(t *testing.T) {
			changed := request
			mutate(&changed)
			if err := admitOwnerAction(f.root, "dev", f.request.Binding, changed); err == nil {
				t.Fatal("foreign action admitted")
			}
		})
	}
	attempt.State = session.ActionAttemptIndeterminate
	writePrivateImportFixture(t, f.root, filepath.Join("action-attempts", f.record.ID), attemptID+".json", attempt)
	if err := admitOwnerAction(f.root, "dev", f.request.Binding, request); err == nil {
		t.Fatal("terminal attempt admitted for guest execution")
	}
}
