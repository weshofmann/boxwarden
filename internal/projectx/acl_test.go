package projectx

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type inspectorFunc func(string) (bool, error)

func (f inspectorFunc) HasExtendedACL(p string) (bool, error) { return f(p) }
func TestMain(m *testing.M) {
	aclInspector = inspectorFunc(func(string) (bool, error) { return false, nil })
	os.Exit(m.Run())
}
func TestStoreFailsClosedForACLAndInspectionFailure(t *testing.T) {
	for _, kind := range []string{"root", "directory", "record", "inspection", "temporary"} {
		t.Run(kind, func(t *testing.T) {
			root := privateRoot(t)
			if e := Create(root, "work", fixtureRecord()); e != nil {
				t.Fatal(e)
			}
			path := recordPath(root)
			if kind == "root" {
				path = root
			}
			if kind == "directory" {
				path = filepath.Join(root, "projects")
			}
			aclInspector = inspectorFunc(func(p string) (bool, error) {
				if kind == "inspection" && p == path {
					return false, errors.New("inspection failed")
				}
				return p == path && kind != "temporary" || kind == "temporary" && strings.Contains(p, ".tmp-"), nil
			})
			t.Cleanup(func() { aclInspector = inspectorFunc(func(string) (bool, error) { return false, nil }) })
			if kind != "temporary" {
				if _, e := Load(root, "work", "dev"); e == nil {
					t.Fatal("ACL admitted")
				}
			}
			if e := Save(root, "work", fixtureRecord()); e == nil {
				t.Fatal("ACL write admitted")
			}
		})
	}
}
func TestSetupRetryFinishesDirectorySync(t *testing.T) {
	root := privateRoot(t)
	if e := Create(root, "work", fixtureRecord()); e != nil {
		t.Fatal(e)
	}
	original := syncProjectDirectory
	syncProjectDirectory = func(dir *os.Root) error {
		if dir.Name() == filepath.Join(root, "projects") {
			return errors.New("sync failure")
		}
		return original(dir)
	}
	t.Cleanup(func() { syncProjectDirectory = original })
	if e := SaveSetup(root, fixtureSetup()); e == nil {
		t.Fatal("sync failure hidden")
	}
	if got, e := LoadSetup(root); e != nil || got != fixtureSetup() {
		t.Fatalf("published setup missing: %#v %v", got, e)
	}
	syncProjectDirectory = original
	if e := SaveSetup(root, fixtureSetup()); e != nil {
		t.Fatalf("retry failed: %v", e)
	}
	entries, e := os.ReadDir(filepath.Join(root, "projects"))
	if e != nil || len(entries) != 2 {
		t.Fatalf("temporary records leaked: %v %v", entries, e)
	}
}

// A failed parent fsync leaves a visible projects directory. A retry must
// still sync its parent rather than infer durability from that visibility.
func TestSetupRetryDoesNotSkipParentDirectorySync(t *testing.T) {
	root := privateRoot(t)
	original := syncProjectDirectory
	syncProjectDirectory = func(dir *os.Root) error {
		if dir.Name() == root {
			return errors.New("parent sync failure")
		}
		return original(dir)
	}
	t.Cleanup(func() { syncProjectDirectory = original })
	for attempt := range 2 {
		if err := SaveSetup(root, fixtureSetup()); err == nil {
			t.Fatalf("attempt %d hid parent sync failure", attempt)
		}
	}
	syncProjectDirectory = original
	if err := SaveSetup(root, fixtureSetup()); err != nil {
		t.Fatalf("durable retry: %v", err)
	}
}
