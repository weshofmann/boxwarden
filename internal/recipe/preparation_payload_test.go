package recipe

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPreparationPayloadContainsOnlyReusableGuestIntent(t *testing.T) {
	value, err := Load(writeRecipe(t, validRecipe))
	if err != nil {
		t.Fatal(err)
	}
	value.Steps = append(value.Steps, Step{ID: "base-tool", Phase: "prepare", Argv: []string{"/bin/echo", "one word", "; literal"}})
	raw, err := PreparationPayload(value, strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	want := `{"version":1,"preparation_key":"` + strings.Repeat("a", 64) + `","apt_packages":["git","nodejs"],"steps":[{"id":"base-tool","argv":["/bin/echo","one word","; literal"]}]}`
	if string(raw) != want {
		t.Fatalf("preparation payload = %s", raw)
	}
	if !json.Valid(raw) || strings.Contains(string(raw), "install-chatgpt") {
		t.Fatalf("session step leaked into reusable preparation: %s", raw)
	}
}

func TestPreparationPayloadRejectsInvalidRecipeOrKey(t *testing.T) {
	value, err := Load(writeRecipe(t, validRecipe))
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"", strings.Repeat("A", 64), strings.Repeat("a", 63)} {
		if _, err := PreparationPayload(value, key); err == nil {
			t.Fatalf("invalid key %q accepted", key)
		}
	}
	value.AptPackages = []string{"git;reboot"}
	if _, err := PreparationPayload(value, strings.Repeat("a", 64)); err == nil {
		t.Fatal("invalid package accepted")
	}
}
