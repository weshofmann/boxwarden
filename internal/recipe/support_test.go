package recipe

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func supportRecipe(t *testing.T) Recipe {
	t.Helper()
	r, err := LoadRunnable(filepath.Join("..", "..", "examples", "v0.2-alpha-actions.json"))
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func TestGuestSupportAddsPinnedChecksWithoutMutatingRecipe(t *testing.T) {
	r := supportRecipe(t)
	before, _, _ := CanonicalIntent(r)
	root, _ := filepath.Abs(filepath.Join("..", "..", "guest", "ubuntu-24.04-arm64"))
	got, err := WithGuestSupport(r, root)
	if err != nil {
		t.Fatal(err)
	}
	again, err := WithGuestSupport(r, root)
	if err != nil || !reflect.DeepEqual(got, again) {
		t.Fatalf("nondeterministic support recipe: %v", err)
	}
	after, _, _ := CanonicalIntent(r)
	if !bytes.Equal(before, after) {
		t.Fatal("input recipe mutated")
	}
	if len(got.Steps) != len(r.Steps)+2 || got.Steps[0].ID != "boxwarden-support-prepare" || got.Steps[0].Phase != "prepare" || got.Steps[1].ID != "boxwarden-support-startup" || got.Steps[1].Phase != "startup" {
		t.Fatalf("support steps missing: %+v", got.Steps)
	}
	for _, step := range got.Steps[:2] {
		if !strings.Contains(strings.Join(step.Argv, " "), "33b12b9e293bcbdf7d6c91f933b42003ca394e7a678ac83b8d80df714d27c57e") {
			t.Fatal("bootstrap lock missing from step")
		}
	}
	_, originalIntent, _ := CanonicalIntent(r)
	_, newIntent, _ := CanonicalIntent(got)
	definition, _ := GuestDefinitionDigest(root)
	originalKey, _ := PreparationKey(r, definition)
	newKey, _ := PreparationKey(got, definition)
	if originalIntent == newIntent || originalKey == newKey {
		t.Fatal("support checks did not change preparation and intent")
	}
}
func TestGuestSupportRejectsReservedActions(t *testing.T) {
	root, _ := filepath.Abs(filepath.Join("..", "..", "guest", "ubuntu-24.04-arm64"))
	for _, phase := range []string{"prepare", "startup", "once"} {
		r := supportRecipe(t)
		r.Steps = append(r.Steps, Step{ID: "boxwarden-support-forged", Phase: phase, Argv: []string{"/usr/bin/true"}})
		if _, err := WithGuestSupport(r, root); err == nil {
			t.Fatalf("reserved %s action accepted", phase)
		}
	}
	r := supportRecipe(t)
	r.Launch = append(r.Launch, Launch{ID: "boxwarden-support-forged", Argv: []string{"/usr/bin/true"}})
	if _, err := WithGuestSupport(r, root); err == nil {
		t.Fatal("reserved launch accepted")
	}
}
func TestGuestSupportRejectsChangedOrLinkedSourcePins(t *testing.T) {
	source, _ := filepath.Abs(filepath.Join("..", "..", "guest", "ubuntu-24.04-arm64"))
	for _, changed := range []string{"artifacts/boxwarden-guest-bootstrap", "clipboard.py", "support-check.py"} {
		t.Run(changed, func(t *testing.T) {
			root := t.TempDir()
			for _, name := range []string{"artifacts.lock.json", "artifacts/boxwarden-guest-bootstrap", "clipboard.py", "support-check.py"} {
				data, err := os.ReadFile(filepath.Join(source, name))
				if err != nil {
					t.Fatal(err)
				}
				target := filepath.Join(root, name)
				if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(target, data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			target := filepath.Join(root, changed)
			if changed == "artifacts/boxwarden-guest-bootstrap" {
				if err := os.WriteFile(target, []byte("changed"), 0600); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.Remove(target); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(filepath.Join(source, changed), target); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := WithGuestSupport(supportRecipe(t), root); err == nil {
				t.Fatal("unsafe source accepted")
			}
		})
	}
}
func TestGuestDefinitionIncludesSupportChecker(t *testing.T) {
	for _, name := range guestDefinitionFiles {
		if name == "support-check.py" {
			return
		}
	}
	t.Fatal("support checker missing from definition identity")
}
