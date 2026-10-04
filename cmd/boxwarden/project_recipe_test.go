package main

import (
	"encoding/json"
	"github.com/weshofmann/boxwarden/internal/config"
	"github.com/weshofmann/boxwarden/internal/projectx"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/weshofmann/boxwarden/internal/app"
	"github.com/weshofmann/boxwarden/internal/recipe"
	"github.com/weshofmann/boxwarden/internal/session"
)

func TestProjectPreparationCapturesSupportAndPreservesOriginalIntentOnReplacement(t *testing.T) {
	source, err := filepath.Abs("../../examples/v0.2-alpha-chatgpt.json")
	if err != nil {
		t.Fatal(err)
	}
	definition, err := filepath.Abs("../../guest/ubuntu-24.04-arm64")
	if err != nil {
		t.Fatal(err)
	}
	original, err := recipe.LoadRunnable(source)
	if err != nil {
		t.Fatal(err)
	}
	value, err := loadPreparationRecipe(t.TempDir(), app.AlphaPrepareInput{RecipePath: source, GuestDefinitionRoot: definition, RequireGuestSupport: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(value.Steps) != len(original.Steps)+2 || value.Steps[0].ID != recipe.GuestSupportPrepareID || value.Steps[1].ID != recipe.GuestSupportStartupID {
		t.Fatalf("support missing: %+v", value.Steps)
	}
	if !reflect.DeepEqual(value.Steps[2:], original.Steps) {
		t.Fatal("software intent altered")
	}
	state := t.TempDir()
	if err := os.Chmod(state, 0700); err != nil {
		t.Fatal(err)
	}
	digest, err := session.PublishRecipeIntent(state, value)
	if err != nil {
		t.Fatal(err)
	}
	refreshed, err := loadPreparationRecipe(state, app.AlphaPrepareInput{CapturedIntentDigest: digest, GuestDefinitionRoot: definition, RequireGuestSupport: true})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(value, refreshed) {
		t.Fatal("unchanged replacement duplicated support or changed software intent")
	}
}

func TestProjectPreparationRejectsUnsupportedWorkspaceBeforeBuilding(t *testing.T) {
	source := filepath.Join("..", "..", "examples", "v0.2-alpha-base.json")
	value, err := recipe.LoadRunnable(source)
	if err != nil {
		t.Fatal(err)
	}
	value.Workspaces[0].Mount = "/home/boxwarden/workspaces/other"
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	filename := filepath.Join(t.TempDir(), "recipe.json")
	if err := os.WriteFile(filename, raw, 0600); err != nil {
		t.Fatal(err)
	}
	definition, _ := filepath.Abs("../../guest/ubuntu-24.04-arm64")
	if _, err := loadPreparationRecipe(t.TempDir(), app.AlphaPrepareInput{RecipePath: filename, GuestDefinitionRoot: definition, RequireGuestSupport: true}); err == nil {
		t.Fatal("unsupported workspace admitted")
	}
}

func TestProjectRecipePrerequisitesRejectDriftedToolBeforeISOAndFormatter(t *testing.T) {
	root := t.TempDir()
	setup := projectx.Setup{Version: 2, SourceRoot: root, FormatterBundle: filepath.Join(root, "missing-formatter"), ISOPath: filepath.Join(root, "missing.iso"), GoBinary: filepath.Join(root, "go"), OpenSSLPath: filepath.Join(root, "openssl"), OpenSSLSHA256: strings.Repeat("a", 64), XorrisoPath: filepath.Join(root, "xorriso"), XorrisoSHA256: strings.Repeat("b", 64)}
	for _, path := range []string{setup.GoBinary, setup.OpenSSLPath, setup.XorrisoPath} {
		if err := os.WriteFile(path, []byte("synthetic executable"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	err := checkProjectSetup(t.Context(), config.Domain{}, setup)
	if err == nil || !strings.Contains(err.Error(), "recipe preparation tools") {
		t.Fatalf("tool drift hidden by expensive or later prerequisite: %v", err)
	}
}
