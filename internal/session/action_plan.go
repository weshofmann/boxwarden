package session

import (
	"errors"
	"fmt"

	"github.com/weshofmann/boxwarden/internal/recipe"
)

var ErrAutomaticActionUnresolved = errors.New("automatic action has an unresolved attempt")

// PlanAutomaticActions derives the remaining ordered guest steps from one
// exact running session and its durable attempts. It never authorizes a retry
// of a reserved or indeterminate attempt, including an older startup attempt
// on the same system.
func PlanAutomaticActions(record Record, intent recipe.Recipe, attempts []ActionAttempt) ([]recipe.Step, error) {
	if err := validateRecordForStore(record.Domain, record); err != nil {
		return nil, fmt.Errorf("invalid automatic action session: %w", err)
	}
	if record.IntendedState != StateRunning || record.Readiness.Status != ReadinessReady ||
		record.Backend.Kind != "tart" || !validUUID(record.StartGeneration) || !lowerSHA256(record.RecipeIntentDigest) {
		return nil, fmt.Errorf("automatic actions require a recipe-bound running session")
	}
	_, digest, err := recipe.CanonicalIntent(intent)
	if err != nil {
		return nil, fmt.Errorf("invalid automatic action recipe: %w", err)
	}
	if digest != record.RecipeIntentDigest {
		return nil, fmt.Errorf("automatic action recipe differs from session intent")
	}
	steps := make([]recipe.Step, 0, len(intent.Steps))
	for _, phase := range []string{"once", "startup"} {
		for _, step := range intent.Steps {
			if step.Phase == phase {
				steps = append(steps, step)
			}
		}
	}
	completed := make(map[string]bool, len(steps))
	seen := make(map[string]bool, len(steps))
	for _, attempt := range attempts {
		if err := validateActionAttempt(attempt); err != nil {
			return nil, fmt.Errorf("automatic action journal contains an invalid attempt: %w", err)
		}
		if attempt.Domain != record.Domain || attempt.SessionName != string(record.Name) || attempt.SessionID != record.ID {
			return nil, fmt.Errorf("automatic action journal contains a foreign attempt")
		}
		if attempt.BackendObject != record.Backend.ObjectID {
			continue
		}
		if attempt.State == ActionAttemptReserved || attempt.State == ActionAttemptIndeterminate || attempt.State == ActionAttemptFailed {
			return nil, fmt.Errorf("%w: %s/%s attempt %s is %s", ErrAutomaticActionUnresolved,
				attempt.ActionPhase, attempt.ActionID, attempt.AttemptID, attempt.State)
		}
		if attempt.ActionPhase != "once" && attempt.ActionPhase != "startup" {
			continue
		}
		if attempt.RecipeDigest != record.RecipeIntentDigest ||
			(attempt.ActionPhase == "startup" && attempt.Generation != record.StartGeneration) {
			continue
		}
		key := attempt.ActionPhase + "/" + attempt.ActionID
		if seen[key] {
			return nil, fmt.Errorf("automatic action %s has conflicting attempts", key)
		}
		seen[key] = true
		completed[key] = attempt.State == ActionAttemptSucceeded || attempt.State == ActionAttemptSkipped
	}
	pending := make([]recipe.Step, 0, len(steps))
	for _, step := range steps {
		key := step.Phase + "/" + step.ID
		if completed[key] {
			if len(pending) != 0 {
				return nil, fmt.Errorf("automatic action %s completed after an earlier step was missing", key)
			}
			continue
		}
		pending = append(pending, step)
	}
	return pending, nil
}
