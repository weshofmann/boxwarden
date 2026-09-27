package alphaprep

import (
	"path/filepath"
	"regexp"
	"testing"

	"github.com/weshofmann/boxwarden/internal/recipe"
)

func TestNewRequestDerivesPrivateDomainAttemptAndFreshCandidateIDs(t *testing.T) {
	_, selected := preflightFixture(t)
	value := recipe.Recipe{Version: 1}
	iso := filepath.Join(selected.StateRoot, "ubuntu.iso")
	guest := filepath.Join(selected.StateRoot, "guest-definition")
	first, err := NewRequest(selected, value, iso, guest)
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewRequest(selected, value, iso, guest)
	if err != nil {
		t.Fatal(err)
	}
	if first.StateRoot != selected.StateRoot || first.Inputs.AttemptRoot != filepath.Join(selected.StateRoot, "prepared-attempts") || first.Inputs.ISOPath != iso || first.Inputs.GuestDefinitionRoot != guest || first.Inputs.Recipe.Version != 1 {
		t.Fatalf("request paths or intent = %+v", first)
	}
	if first.Inputs.AttemptID == second.Inputs.AttemptID || first.Inputs.CandidateID == second.Inputs.CandidateID || first.Inputs.RunID == second.Inputs.RunID {
		t.Fatal("fresh invocation reused build identity")
	}
	for name, value := range map[string]string{"attempt": first.Inputs.AttemptID, "candidate": first.Inputs.CandidateID, "run": first.Inputs.RunID} {
		if !regexp.MustCompile(`^[a-z-]+[0-9a-f]{12,32}$`).MatchString(value) {
			t.Fatalf("%s identity is malformed: %q", name, value)
		}
	}
	if _, err := NewRequest(selected, value, "relative.iso", guest); err == nil {
		t.Fatal("relative ISO path accepted")
	}
}
