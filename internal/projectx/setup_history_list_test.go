package projectx

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestListAdmitsExactPrivateSetupHistoryAfterUpdate(t *testing.T) {
	root := privateRoot(t)
	if err := SaveSetup(root, fixtureSetup()); err != nil {
		t.Fatal(err)
	}
	if err := Create(root, "work", fixtureRecord()); err != nil {
		t.Fatal(err)
	}
	next := fixtureSetup()
	next.SourceRoot = "/new/source"
	history, err := UpdateSetup(root, next)
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(history)
	if err != nil {
		t.Fatal(err)
	}
	got, err := List(root, "work")
	if err != nil || len(got) != 1 || got[0].Name != "dev" {
		t.Fatalf("list after update %v %v", got, err)
	}
	after, err := os.ReadFile(history)
	if err != nil || string(before) != string(after) {
		t.Fatal("history changed during list")
	}
}

func TestListRejectsUnsafeOrForgedSetupHistory(t *testing.T) {
	for _, kind := range []string{"uppercase", "short", "nonhex", "digest", "schema", "symlink", "mode", "acl"} {
		t.Run(kind, func(t *testing.T) {
			root := privateRoot(t)
			if err := SaveSetup(root, fixtureSetup()); err != nil {
				t.Fatal(err)
			}
			raw := append(mustJSON(t, fixtureSetup()), '\n')
			if kind == "schema" {
				raw = []byte("{}\n")
			}
			digest := fmt.Sprintf("%x", sha256.Sum256(raw))
			switch kind {
			case "uppercase":
				digest = strings.ToUpper(digest)
			case "short":
				digest = digest[:63]
			case "nonhex":
				digest = "z" + digest[1:]
			case "digest":
				digest = strings.Repeat("0", 64)
			}
			history := filepath.Join(root, "projects", ".setup-history-"+digest+".json")
			if kind == "symlink" {
				if err := os.Symlink(filepath.Join(root, "projects", setupName), history); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.WriteFile(history, raw, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if kind == "mode" {
				if err := os.Chmod(history, 0644); err != nil {
					t.Fatal(err)
				}
			}
			original := aclInspector
			if kind == "acl" {
				aclInspector = inspectorFunc(func(path string) (bool, error) { return path == history, nil })
				defer func() { aclInspector = original }()
			}
			if _, err := List(root, "work"); err == nil {
				t.Fatal("unsafe or forged history accepted")
			}
		})
	}
}

func TestListIgnoresInterruptedSetupHistoryTemporaryPublication(t *testing.T) {
	root := privateRoot(t)
	if err := SaveSetup(root, fixtureSetup()); err != nil {
		t.Fatal(err)
	}
	if err := Create(root, "work", fixtureRecord()); err != nil {
		t.Fatal(err)
	}
	temporary := filepath.Join(root, "projects", ".setup-history-"+strings.Repeat("a", 64)+".json.tmp-"+strings.Repeat("b", 32))
	if err := os.WriteFile(temporary, []byte("interrupted incomplete write"), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := List(root, "work")
	if err != nil || len(got) != 1 {
		t.Fatalf("interrupted publication blocked read-only list: %v %v", got, err)
	}
}

func TestListRefusesForgedSetupHistoryTemporaryKeys(t *testing.T) {
	hash := strings.Repeat("a", 64)
	nonce := strings.Repeat("b", 32)
	for _, key := range []string{
		".unknown-history-" + hash + ".json.tmp-" + nonce,
		".setup-history-" + strings.ToUpper(hash) + ".json.tmp-" + nonce,
		".setup-history-" + hash[:63] + ".json.tmp-" + nonce,
		".setup-history-" + hash + ".json.tmp-" + strings.ToUpper(nonce),
		".setup-history-" + hash + ".json.tmp-" + nonce[:31],
	} {
		t.Run(key, func(t *testing.T) {
			root := privateRoot(t)
			if err := SaveSetup(root, fixtureSetup()); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, "projects", key), []byte("incomplete"), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := List(root, "work"); err == nil {
				t.Fatal("forged temporary key hidden")
			}
		})
	}
}
