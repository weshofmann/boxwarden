package recipe

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestPreparationKeyTracksOnlyBaseInputs(t *testing.T) {
	recipe, err := Load(writeRecipe(t, validRecipe))
	if err != nil {
		t.Fatal(err)
	}
	definition := strings.Repeat("a", 64)
	base, err := PreparationKey(recipe, definition)
	if err != nil || len(base) != 64 {
		t.Fatalf("base key = %q, %v", base, err)
	}
	changedSession := recipe
	changedSession.Steps = append([]Step(nil), recipe.Steps...)
	changedSession.Steps[0].Argv = []string{"/bin/true"}
	changedSession.Workspaces = nil
	changedSession.Launch = nil
	got, err := PreparationKey(changedSession, definition)
	if err != nil || got != base {
		t.Fatalf("session-only inputs changed base key: %q, %v", got, err)
	}
	changedPackages := recipe
	changedPackages.AptPackages = append([]string(nil), recipe.AptPackages...)
	changedPackages.AptPackages[0] = "curl"
	got, err = PreparationKey(changedPackages, definition)
	if err != nil || got == base {
		t.Fatalf("package change did not invalidate base key: %q, %v", got, err)
	}
	changedPrepare := recipe
	changedPrepare.Steps = append(append([]Step(nil), recipe.Steps...), Step{ID: "base-tool", Phase: "prepare", Argv: []string{"/bin/true"}})
	got, err = PreparationKey(changedPrepare, definition)
	if err != nil || got == base {
		t.Fatalf("prepare step did not invalidate base key: %q, %v", got, err)
	}
	got, err = PreparationKey(recipe, strings.Repeat("b", 64))
	if err != nil || got == base {
		t.Fatalf("guest definition change did not invalidate base key: %q, %v", got, err)
	}
}

func TestTrackedAlphaRecipeAndGuestDefinitionProduceCacheKey(t *testing.T) {
	example := filepath.Join("..", "..", "examples", "v0.2", "ubuntu-desktop.json")
	definition := filepath.Join("..", "..", "guest", "ubuntu-24.04-arm64")
	recipe, err := Load(example)
	if err != nil {
		t.Fatal(err)
	}
	absolute, err := filepath.Abs(definition)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := GuestDefinitionDigest(absolute)
	if err != nil {
		t.Fatal(err)
	}
	key, err := PreparationKey(recipe, digest)
	if err != nil || len(key) != 64 {
		t.Fatalf("tracked cache key = %q, %v", key, err)
	}
}

func TestPreparationKeyRejectsUnadmittedInputs(t *testing.T) {
	recipe, err := Load(writeRecipe(t, validRecipe))
	if err != nil {
		t.Fatal(err)
	}
	for _, digest := range []string{"", "abc", strings.Repeat("Z", 64)} {
		if _, err := PreparationKey(recipe, digest); err == nil {
			t.Fatalf("invalid definition digest %q was accepted", digest)
		}
	}
	recipe.Version = 99
	if _, err := PreparationKey(recipe, strings.Repeat("a", 64)); err == nil {
		t.Fatal("unsupported recipe was assigned a cache key")
	}
}
