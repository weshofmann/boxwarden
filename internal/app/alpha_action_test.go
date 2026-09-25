package app

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/weshofmann/boxwarden/internal/config"
	"github.com/weshofmann/boxwarden/internal/recipe"
	"github.com/weshofmann/boxwarden/internal/session"
)

func TestAlphaActionListRecoversAttemptIDFromExactSessionJournal(t *testing.T) {
	configPath, selected := writeV2DomainFixture(t, "alpha")
	value := recipe.Recipe{Version: 1,
		Source:  recipe.Source{Kind: "ubuntu-24.04.4-desktop-arm64", SHA256: "c2610520bf582976839a1724c669e1cfed0547427be5a0ad12d457b92b46ffbe"},
		Machine: recipe.Machine{CPUs: 4, MemoryMiB: 4096, SystemDiskGiB: 30},
		Steps:   []recipe.Step{{ID: "probe", Phase: "reconfigure", Argv: []string{"/usr/bin/true"}}},
	}
	digest, err := session.PublishRecipeIntent(selected.StateRoot, value)
	if err != nil {
		t.Fatal(err)
	}
	record := session.Record{Version: 2, Domain: selected.ID, Name: "dev",
		ID: "13b0bf73-3bd5-4f1c-8bdc-71d50c36d6d0", Mode: session.ModeClean,
		IntendedState: session.StateRunning, Backend: session.BackendRef{Kind: "tart", ObjectID: "boxwarden-alpha-dev"},
		GoldenRevision: "golden-r1", RecipeIntentDigest: digest,
		StartGeneration: "00000000-0000-4000-8000-000000000003",
		Readiness:       session.ReadinessRecord{Status: session.ReadinessReady}}
	if err := session.SaveRecord(selected.StateRoot, selected.ID, record); err != nil {
		t.Fatal(err)
	}
	attempt := session.ActionAttempt{Version: 1, Domain: selected.ID, SessionName: "dev", SessionID: record.ID,
		BackendObject: record.Backend.ObjectID, Generation: record.StartGeneration, RecipeDigest: digest,
		ActionID: "probe", ActionPhase: "reconfigure", AttemptID: "00112233-4455-4677-8899-aabbccddeeff",
		State: session.ActionAttemptReserved}
	if err := session.ReserveActionAttempt(selected.StateRoot, attempt); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	args := []string{"--config", configPath, "--domain", "alpha", "session", "action", "list", "dev"}
	if err := Run(t.Context(), args, Options{Output: &output}); err != nil ||
		!strings.Contains(output.String(), "attempt: "+attempt.AttemptID+"\n") ||
		!strings.Contains(output.String(), "action: reconfigure/probe\n") ||
		!strings.Contains(output.String(), "state: reserved\n") {
		t.Fatalf("list did not recover exact attempt: output=%q error=%v", output.String(), err)
	}
	output.Reset()
	if err := Run(t.Context(), append(args[:7], "missing"), Options{Output: &output}); err == nil || output.Len() != 0 {
		t.Fatalf("missing session action journal was shown: output=%q error=%v", output.String(), err)
	}
}

func TestAlphaActionListRejectsForeignEntryBeforePrinting(t *testing.T) {
	var output bytes.Buffer
	selected := config.Domain{ID: "alpha"}
	attempt := session.ActionAttempt{Domain: "work", SessionName: "dev", AttemptID: "00112233-4455-4677-8899-aabbccddeeff", ActionID: "probe"}
	if err := writeAlphaActionList(&output, selected, "dev", []session.ActionAttempt{attempt}); err == nil || output.Len() != 0 {
		t.Fatalf("foreign action printed partial list: output=%q error=%v", output.String(), err)
	}
}

func TestAlphaActionCommandsRouteExactIntentAndReportOutcome(t *testing.T) {
	configPath, selected := writeV2DomainFixture(t, "alpha")
	const attemptID = "00112233-4455-4677-8899-aabbccddeeff"
	base := []string{"--config", configPath, "--domain", "alpha", "session", "action"}
	var output bytes.Buffer
	called := 0
	options := Options{Output: &output, AlphaAction: func(_ context.Context, actual config.Domain, input AlphaActionInput) (session.ActionAttempt, error) {
		called++
		if actual != selected || input.SessionName != "dev" {
			t.Fatalf("action lost selected domain or session: %+v %+v", actual, input)
		}
		result := session.ActionAttempt{Version: 1, Domain: selected.ID, SessionName: "dev", AttemptID: attemptID,
			ActionPhase: "once", ActionID: "configure-agent"}
		switch input.Operation {
		case "run":
			if input.Phase != "once" || input.ActionID != "configure-agent" || input.AttemptID != "" {
				t.Fatalf("changed run intent: %+v", input)
			}
			result.State = session.ActionAttemptIndeterminate
			return result, errors.New("guest receipt unavailable")
		case "retry":
			if input.AttemptID != attemptID || input.Phase != "" || input.ActionID != "" {
				t.Fatalf("changed retry intent: %+v", input)
			}
			result.State = session.ActionAttemptSucceeded
			result.ReceiptSHA256 = strings.Repeat("a", 64)
			return result, nil
		case "skip":
			if input.AttemptID != attemptID || input.Phase != "" || input.ActionID != "" {
				t.Fatalf("changed skip intent: %+v", input)
			}
			result.State = session.ActionAttemptSkipped
			return result, nil
		default:
			t.Fatalf("unknown operation: %+v", input)
			return session.ActionAttempt{}, nil
		}
	}}
	if err := Run(t.Context(), append(base, "run", "once", "configure-agent", "dev"), options); err == nil ||
		!strings.Contains(output.String(), "attempt: "+attemptID+"\n") || !strings.Contains(output.String(), "state: indeterminate\n") ||
		!strings.Contains(output.String(), "session action retry "+attemptID+" dev;") {
		t.Fatalf("uncertain run hid attempt: output=%q error=%v", output.String(), err)
	}
	output.Reset()
	if err := Run(t.Context(), append(base, "retry", attemptID, "dev"), options); err != nil ||
		!strings.Contains(output.String(), "state: succeeded\n") || !strings.Contains(output.String(), "receipt-sha256: "+strings.Repeat("a", 64)+"\n") {
		t.Fatalf("exact retry result: output=%q error=%v", output.String(), err)
	}
	output.Reset()
	if err := Run(t.Context(), append(base, "skip", attemptID, "dev"), options); err != nil ||
		!strings.Contains(output.String(), "state: skipped\n") || strings.Contains(output.String(), "receipt-sha256") {
		t.Fatalf("exact skip overclaimed: output=%q error=%v", output.String(), err)
	}
	for _, suffix := range [][]string{
		{"run", "prepare", "configure-agent", "dev"},
		{"run", "once", "configure-agent", "bad/name"},
		{"retry", "bad-uuid", "dev"},
		{"skip", attemptID, "bad/name"},
	} {
		if err := Run(t.Context(), append(base, suffix...), options); err == nil || called != 3 {
			t.Fatalf("invalid action reached callback: %v; calls=%d", err, called)
		}
	}
	workConfig, _ := writeV2DomainFixture(t, "work")
	if err := Run(t.Context(), []string{"--config", workConfig, "--domain", "work", "session", "action", "skip", attemptID, "dev"}, options); err == nil || called != 3 {
		t.Fatalf("non-alpha domain reached action callback: %v; calls=%d", err, called)
	}
}

func TestAlphaActionRejectsFalseSuccessFromComposition(t *testing.T) {
	configPath, selected := writeV2DomainFixture(t, "alpha")
	args := []string{"--config", configPath, "--domain", "alpha", "session", "action", "run", "once", "configure-agent", "dev"}
	for label, mutate := range map[string]func(*session.ActionAttempt){
		"foreign domain": func(a *session.ActionAttempt) { a.Domain = "work" },
		"wrong action":   func(a *session.ActionAttempt) { a.ActionID = "different" },
		"no receipt":     func(a *session.ActionAttempt) { a.ReceiptSHA256 = "" },
		"false skipped":  func(a *session.ActionAttempt) { a.State = session.ActionAttemptSkipped; a.ReceiptSHA256 = "" },
	} {
		t.Run(label, func(t *testing.T) {
			var output bytes.Buffer
			options := Options{Output: &output, AlphaAction: func(_ context.Context, actual config.Domain, _ AlphaActionInput) (session.ActionAttempt, error) {
				if actual != selected {
					t.Fatalf("foreign configuration: %+v", actual)
				}
				result := session.ActionAttempt{Version: 1, Domain: selected.ID, SessionName: "dev",
					AttemptID: "00112233-4455-4677-8899-aabbccddeeff", ActionPhase: "once", ActionID: "configure-agent",
					State: session.ActionAttemptSucceeded, ReceiptSHA256: strings.Repeat("a", 64)}
				mutate(&result)
				return result, nil
			}}
			if err := Run(t.Context(), args, options); err == nil || output.Len() != 0 {
				t.Fatalf("false action success published: output=%q err=%v", output.String(), err)
			}
		})
	}
	var output bytes.Buffer
	if err := Run(t.Context(), args, Options{Output: &output, AlphaAction: func(context.Context, config.Domain, AlphaActionInput) (session.ActionAttempt, error) {
		return session.ActionAttempt{}, nil
	}}); err == nil || output.Len() != 0 {
		t.Fatalf("empty action result accepted as success: output=%q err=%v", output.String(), err)
	}
}
