package app

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/weshofmann/boxwarden/internal/config"
	"github.com/weshofmann/boxwarden/internal/projectx"
	"github.com/weshofmann/boxwarden/internal/session"
)

// These aliases are packaged, already runnable recipes, not arbitrary host
// command/file input. Preparation remains owned by the admitted alpha path.
func projectRecipeFile(name string) (string, error) {
	switch name {
	case "desktop":
		return "v0.2-alpha-base.json", nil
	case "actions":
		return "v0.2-alpha-actions.json", nil
	case "chatgpt":
		return "v0.2-alpha-chatgpt.json", nil
	default:
		return "", errors.New("supported project recipes: desktop, actions, chatgpt")
	}
}

func prepareProjectRecipe(ctx context.Context, c parsedCommand, loaded config.Config, d config.Domain, setup projectx.Setup, name, captured string, o Options) (string, string, error) {
	if setup.Version != 2 && setup.Version != 3 {
		return "", "", errors.New("recipe preparation prerequisites are missing; rerun this package's prepare-projects.sh with the exact OpenSSL and xorriso executables")
	}
	if o.AlphaPrepare == nil {
		return "", "", errors.New("admitted recipe preparer is required")
	}
	input := AlphaPrepareInput{ISOPath: setup.ISOPath, GuestDefinitionRoot: filepath.Join(setup.SourceRoot, "guest", "ubuntu-24.04-arm64"), OpenSSLPath: setup.OpenSSLPath, OpenSSLSHA256: setup.OpenSSLSHA256, XorrisoPath: setup.XorrisoPath, XorrisoSHA256: setup.XorrisoSHA256, RequireGuestSupport: true, CapturedIntentDigest: captured}
	if captured == "" {
		filename, err := projectRecipeFile(name)
		if err != nil {
			return "", "", err
		}
		input.RecipePath = filepath.Join(setup.SourceRoot, "examples", filename)
	}
	if _, err := fmt.Fprintln(o.Output, "project recipe: preparing selected software and guest support; unchanged preparation may be reused"); err != nil {
		return "", "", err
	}
	prepared, err := o.AlphaPrepare(ctx, loaded, d, c.configPath, input)
	if err != nil {
		return "", "", fmt.Errorf("project recipe preparation failed; no project allocation or system replacement started; correct prerequisites or inspect the reported preparation attempt: %w", err)
	}
	if err := validateAlphaPrepared(d, prepared.Base); err != nil {
		return "", "", err
	}
	if !lowerSHA(prepared.IntentDigest) {
		return "", "", errors.New("recipe preparation returned invalid full intent")
	}
	if _, err := session.LoadRecipeIntent(d.StateRoot, prepared.IntentDigest); err != nil {
		return "", "", fmt.Errorf("admit captured project recipe: %w", err)
	}
	if err := writeAlphaPrepared(o.Output, d, prepared.Base); err != nil {
		return "", "", err
	}
	return prepared.Base.Record.CandidateID, prepared.IntentDigest, nil
}
