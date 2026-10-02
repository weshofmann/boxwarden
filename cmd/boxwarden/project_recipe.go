package main

import (
	"errors"
	"fmt"

	"github.com/weshofmann/boxwarden/internal/app"
	"github.com/weshofmann/boxwarden/internal/recipe"
	"github.com/weshofmann/boxwarden/internal/session"
)

// Only project creation or explicit replacement decorates an intent. Ordinary
// open uses the captured session actions without consulting mutable source.
func loadPreparationRecipe(stateRoot string, input app.AlphaPrepareInput) (recipe.Recipe, error) {
	var value recipe.Recipe
	var err error
	if input.CapturedIntentDigest != "" {
		if !input.RequireGuestSupport || input.RecipePath != "" {
			return value, errors.New("captured project intent requires explicit new-system guest support")
		}
		raw, loadErr := session.LoadRecipeIntent(stateRoot, input.CapturedIntentDigest)
		if loadErr != nil {
			return value, loadErr
		}
		value, err = recipe.DecodeIntent(raw)
		if err != nil {
			return value, err
		}
		// This branch receives only the immutable intent of an exactly bound named
		// project. Refresh our two generated checks for the explicitly new system;
		// retain every operator software/action/workspace instruction.
		steps := make([]recipe.Step, 0, len(value.Steps))
		for _, step := range value.Steps {
			if step.ID != recipe.GuestSupportPrepareID && step.ID != recipe.GuestSupportStartupID {
				steps = append(steps, step)
			}
		}
		value.Steps = steps
	} else {
		value, err = recipe.LoadRunnable(input.RecipePath)
		if err != nil {
			return value, err
		}
	}
	if err := recipe.ValidateRunnable(value); err != nil {
		return value, err
	}
	if input.RequireGuestSupport {
		if len(value.Workspaces) != 1 || value.Workspaces[0].Name != "project" || value.Workspaces[0].Mount != "/home/boxwarden/workspaces/project" {
			return value, fmt.Errorf("project recipe requires exactly the project workspace at /home/boxwarden/workspaces/project")
		}
		return recipe.WithGuestSupport(value, input.GuestDefinitionRoot)
	}
	return value, nil
}
