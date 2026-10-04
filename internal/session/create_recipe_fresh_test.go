package session

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/weshofmann/boxwarden/internal/backend/fake"
	"github.com/weshofmann/boxwarden/internal/recipe"
)

func freshRecipeIntent(t *testing.T, root string) string {
	t.Helper()
	value := recipe.Recipe{Version: 1, Source: recipe.Source{Kind: "ubuntu-24.04.4-desktop-arm64", SHA256: "c2610520bf582976839a1724c669e1cfed0547427be5a0ad12d457b92b46ffbe"}, Machine: recipe.Machine{CPUs: 4, MemoryMiB: 4096, SystemDiskGiB: 30}}
	digest, err := PublishRecipeIntent(root, value)
	if err != nil {
		t.Fatal(err)
	}
	return digest
}

// Fresh recipe creation must capture its intent before clone and never adopt
// an existing stopped or interrupted creating session as a fresh result.
func TestFreshRecipeCreateBindsBeforeCloneAndRejectsExistingAllocation(t *testing.T) {
	for _, interrupted := range []bool{false, true} {
		t.Run(map[bool]string{false: "stopped", true: "creating"}[interrupted], func(t *testing.T) {
			d, b, service := createFixture(t)
			creator := service
			digest := freshRecipeIntent(t, d.StateRoot)
			b.SetCloneFault(func(_ context.Context, call fake.CloneCall) error {
				r, err := LoadRecord(d.StateRoot, "work", "dev")
				if err != nil || r.RecipeIntentDigest != digest || r.IntendedState != StateCreating || r.Backend.ObjectID != call.TargetID {
					t.Fatalf("clone without exact durable intent: %+v %v", r, err)
				}
				if interrupted {
					return errors.New("synthetic clone interruption")
				}
				return nil
			})
			created, err := creator.CreateFreshFromRevisionWithIntent(t.Context(), "dev", ModeClean, "golden-r1", digest)
			if interrupted {
				if err == nil || created.Created {
					t.Fatalf("interrupted create returned fresh witness: %+v %v", created, err)
				}
			} else if err != nil || !created.Created || created.Record.RecipeIntentDigest != digest || created.Record.IntendedState != StateStopped {
				t.Fatalf("fresh recipe result %+v %v", created, err)
			}
			retained, err := LoadRecord(d.StateRoot, "work", "dev")
			if err != nil {
				t.Fatal(err)
			}
			reused, err := creator.CreateFreshFromRevisionWithIntent(t.Context(), "dev", ModeClean, "golden-r1", digest)
			if err == nil || reused.Created {
				t.Fatalf("existing allocation adopted %+v %v", reused, err)
			}
			if len(b.CloneCalls()) != 1 {
				t.Fatal("fresh retry cloned another system")
			}
			if after, err := LoadRecord(d.StateRoot, "work", "dev"); err != nil || after != retained {
				t.Fatal("fresh retry changed existing allocation")
			}
		})
	}
}

func TestFreshRecipeCreateRejectsInvalidOrMissingIntentBeforeMutation(t *testing.T) {
	for _, kind := range []string{"empty", "malformed", "missing", "corrupt"} {
		t.Run(kind, func(t *testing.T) {
			d, b, service := createFixture(t)
			creator := service
			digest := ""
			switch kind {
			case "malformed":
				digest = "foreign"
			case "missing":
				digest = strings.Repeat("a", 64)
			case "corrupt":
				digest = freshRecipeIntent(t, d.StateRoot)
				if err := os.WriteFile(filepath.Join(d.StateRoot, "recipe-intents", digest+".json"), []byte("corrupt"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			created, err := creator.CreateFreshFromRevisionWithIntent(t.Context(), "dev", ModeClean, "golden-r1", digest)
			if err == nil || created.Created {
				t.Fatalf("invalid intent accepted: %+v %v", created, err)
			}
			if _, err := LoadRecord(d.StateRoot, "work", "dev"); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("invalid intent reserved session: %v", err)
			}
			if len(b.CloneCalls()) != 0 {
				t.Fatal("invalid intent cloned system")
			}
		})
	}
}
