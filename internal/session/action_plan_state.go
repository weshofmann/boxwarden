package session

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/weshofmann/boxwarden/internal/config"
	"github.com/weshofmann/boxwarden/internal/domain"
	"github.com/weshofmann/boxwarden/internal/lock"
	"github.com/weshofmann/boxwarden/internal/recipe"
)

// LoadAutomaticActionPlan reads one exact session, recipe, and attempt set
// under the session lock. A caller that later executes a step must recheck the
// plan under its own transition before reserving that attempt.
func LoadAutomaticActionPlan(ctx context.Context, configured config.Domain, rawName string) (record Record, pending []recipe.Step, err error) {
	domainID, parseErr := domain.Parse(string(configured.ID))
	if parseErr != nil || domainID != configured.ID || strings.TrimSpace(configured.StateRoot) == "" {
		return Record{}, nil, fmt.Errorf("invalid configured domain")
	}
	name, err := ParseName(rawName)
	if err != nil {
		return Record{}, nil, err
	}
	held, err := lock.AcquireSession(ctx, configured.StateRoot, string(domainID), string(name))
	if err != nil {
		return Record{}, nil, fmt.Errorf("acquire session lock: %w", err)
	}
	defer func() { err = errors.Join(err, held.Release()) }()
	record, err = LoadRecord(configured.StateRoot, string(domainID), string(name))
	if err != nil {
		return Record{}, nil, err
	}
	if record.RecipeIntentDigest == "" {
		return record, []recipe.Step{}, nil
	}
	raw, err := LoadRecipeIntent(configured.StateRoot, record.RecipeIntentDigest)
	if err != nil {
		return Record{}, nil, fmt.Errorf("load exact automatic action recipe: %w", err)
	}
	intent, err := recipe.DecodeIntent(raw)
	if err != nil {
		return Record{}, nil, err
	}
	attempts, err := listActionAttemptsForRecord(configured.StateRoot, record)
	if err != nil {
		return Record{}, nil, fmt.Errorf("load exact automatic action journal: %w", err)
	}
	pending, err = PlanAutomaticActions(record, intent, attempts)
	if err != nil {
		return record, nil, err
	}
	return record, pending, nil
}
