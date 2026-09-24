package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/weshofmann/boxwarden/internal/recipe"
)

func TestPublishRecipeIntentIsImmutableAndRecheckedOnRead(t *testing.T) {
	stateRoot := t.TempDir()
	if err := os.Chmod(stateRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	value := recipe.Recipe{
		Version: 1,
		Source:  recipe.Source{Kind: "ubuntu-24.04.4-desktop-arm64", SHA256: "c2610520bf582976839a1724c669e1cfed0547427be5a0ad12d457b92b46ffbe"},
		Machine: recipe.Machine{CPUs: 4, MemoryMiB: 4096, SystemDiskGiB: 30},
		Steps:   []recipe.Step{{ID: "setup", Phase: "once", Argv: []string{"/bin/echo", "guest only"}}},
	}
	canonical, digest, err := recipe.CanonicalIntent(value)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := LoadRecipeIntent(stateRoot, digest); !os.IsNotExist(err) {
		t.Fatalf("missing intent read = %v, want absence", err)
	}
	if got, err := PublishRecipeIntent(stateRoot, value); err != nil || got != digest {
		t.Fatalf("publish = %q, %v", got, err)
	}
	if got, err := LoadRecipeIntent(stateRoot, digest); err != nil || string(got) != string(canonical) {
		t.Fatalf("read published intent = %q, %v", got, err)
	}
	if got, err := PublishRecipeIntent(stateRoot, value); err != nil || got != digest {
		t.Fatalf("exact retry = %q, %v", got, err)
	}
	path := filepath.Join(stateRoot, "recipe-intents", digest+".json")
	if info, err := os.Lstat(path); err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		t.Fatalf("intent metadata = %v, %v", info, err)
	}
	if err := os.WriteFile(path, []byte("corrupt"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadRecipeIntent(stateRoot, digest); err == nil {
		t.Fatal("corrupt intent was read")
	}
	if _, err := PublishRecipeIntent(stateRoot, value); err == nil {
		t.Fatal("corrupt existing intent was overwritten")
	}
}

func TestLoadRecipeIntentRejectsInvalidDigestAndLink(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadRecipeIntent(root, strings.Repeat("z", 64)); err == nil {
		t.Fatal("invalid digest accepted")
	}
	if err := os.Mkdir(filepath.Join(root, "recipe-intents"), 0o700); err != nil {
		t.Fatal(err)
	}
	const digest = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	if err := os.Symlink(filepath.Join(root, "outside"), filepath.Join(root, "recipe-intents", digest+".json")); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadRecipeIntent(root, digest); err == nil {
		t.Fatal("linked intent accepted")
	}
}
