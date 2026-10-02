package projectx

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestSetupUpdatePreservesExactHistoryAndProjectReceipts(t *testing.T) {
	root := privateRoot(t)
	prior := fixtureSetup()
	if err := SaveSetup(root, prior); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "projects", setupName)
	raw := append([]byte("  "), mustJSON(t, prior)...)
	raw = append(raw, '\n')
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	record := fixtureRecord()
	if err := Create(root, "work", record); err != nil {
		t.Fatal(err)
	}
	receipt, _ := os.ReadFile(recordPath(root))
	next := prior
	next.SourceRoot = "/new/source"
	next.FormatterBundle = "/new/formatter"
	history, err := UpdateSetup(root, next)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, "projects", fmt.Sprintf(".setup-history-%x.json", sha256.Sum256(raw)))
	if history != want {
		t.Fatalf("history %q want %q", history, want)
	}
	got, err := os.ReadFile(history)
	if err != nil || !bytes.Equal(got, raw) {
		t.Fatalf("history changed: %q %v", got, err)
	}
	info, err := os.Stat(history)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("history permissions %v %v", info, err)
	}
	active, err := LoadSetup(root)
	if err != nil || active != next {
		t.Fatalf("active %#v %v", active, err)
	}
	again, err := UpdateSetup(root, next)
	if err != nil || again != "" {
		t.Fatalf("retry %q %v", again, err)
	}
	returned, _ := os.ReadFile(recordPath(root))
	if !bytes.Equal(returned, receipt) {
		t.Fatal("bookmark rewritten")
	}
}

func TestSetupUpdateArchiveFailureLeavesOriginalActive(t *testing.T) {
	for _, kind := range []string{"different", "symlink", "sync"} {
		t.Run(kind, func(t *testing.T) {
			root := privateRoot(t)
			prior := fixtureSetup()
			if err := SaveSetup(root, prior); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(root, "projects", setupName)
			raw, _ := os.ReadFile(path)
			history := filepath.Join(root, "projects", fmt.Sprintf(".setup-history-%x.json", sha256.Sum256(raw)))
			original := syncProjectDirectory
			if kind == "different" {
				if err := os.WriteFile(history, []byte("different\n"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if kind == "symlink" {
				if err := os.Symlink(path, history); err != nil {
					t.Fatal(err)
				}
			}
			if kind == "sync" {
				syncProjectDirectory = func(dir *os.Root) error {
					if dir.Name() == filepath.Join(root, "projects") {
						return errors.New("archive sync failed")
					}
					return original(dir)
				}
				defer func() { syncProjectDirectory = original }()
			}
			next := prior
			next.SourceRoot = "/new/source"
			if _, err := UpdateSetup(root, next); err == nil {
				t.Fatal("archive failure hidden")
			}
			active, err := LoadSetup(root)
			if err != nil || active != prior {
				t.Fatalf("original not retained %#v %v", active, err)
			}
			if kind == "sync" {
				syncProjectDirectory = original
				if _, err := UpdateSetup(root, next); err != nil {
					t.Fatalf("retry %v", err)
				}
			}
		})
	}
}

func TestSetupUpdateRequiresExistingSetup(t *testing.T) {
	if _, err := UpdateSetup(privateRoot(t), fixtureSetup()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("absent profile: %v", err)
	}
}

func TestSetupUpdatePublicationSyncFailureCanFinishByIdenticalRetry(t *testing.T) {
	root := privateRoot(t)
	prior := fixtureSetup()
	if err := SaveSetup(root, prior); err != nil {
		t.Fatal(err)
	}
	next := prior
	next.SourceRoot = "/new/source"
	original := syncProjectDirectory
	calls := 0
	syncProjectDirectory = func(dir *os.Root) error {
		if dir.Name() == filepath.Join(root, "projects") {
			calls++
			if calls == 2 {
				return errors.New("active directory sync failed")
			}
		}
		return original(dir)
	}
	defer func() { syncProjectDirectory = original }()
	history, err := UpdateSetup(root, next)
	if err == nil || history == "" {
		t.Fatalf("active sync error hidden: %q %v", history, err)
	}
	active, err := LoadSetup(root)
	if err != nil || active != next {
		t.Fatalf("published setup %#v %v", active, err)
	}
	syncProjectDirectory = original
	if _, err := UpdateSetup(root, next); err != nil {
		t.Fatalf("same-input retry %v", err)
	}
	saved, err := os.ReadFile(history)
	if err != nil || string(saved) != string(append(mustJSON(t, prior), '\n')) {
		t.Fatalf("previous receipt %s %v", saved, err)
	}
}

func TestSetupUpdateRefusesHistoryACLBeforeChangingActive(t *testing.T) {
	root := privateRoot(t)
	prior := fixtureSetup()
	if err := SaveSetup(root, prior); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(root, "projects", setupName))
	history := filepath.Join(root, "projects", fmt.Sprintf(".setup-history-%x.json", sha256.Sum256(raw)))
	if err := os.WriteFile(history, raw, 0600); err != nil {
		t.Fatal(err)
	}
	original := aclInspector
	aclInspector = inspectorFunc(func(path string) (bool, error) { return path == history, nil })
	defer func() { aclInspector = original }()
	next := prior
	next.SourceRoot = "/new/source"
	if _, err := UpdateSetup(root, next); err == nil {
		t.Fatal("history ACL admitted")
	}
	active, err := LoadSetup(root)
	if err != nil || active != prior {
		t.Fatalf("active changed %#v %v", active, err)
	}
}
