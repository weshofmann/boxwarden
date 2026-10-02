package projectx

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestListDistinguishesMissingStateFromEmptyRegistryAndNeverCreates(t *testing.T) {
	root := privateRoot(t)
	before, _ := os.ReadDir(root)
	got, err := List(root, "work")
	after, _ := os.ReadDir(root)
	if err != nil || len(got) != 0 || !reflect.DeepEqual(before, after) {
		t.Fatalf("empty list mutated root: %v %v", got, err)
	}
	if _, err := List(filepath.Join(root, "unavailable"), "work"); err == nil {
		t.Fatal("missing state treated as empty")
	}
	if err := SaveSetup(root, fixtureSetup()); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"zed", "alice"} {
		r := fixtureRecord()
		r.Name = name
		if err := Create(root, "work", r); err != nil {
			t.Fatal(err)
		}
	}
	before, _ = os.ReadDir(filepath.Join(root, "projects"))
	got, err = List(root, "work")
	after, _ = os.ReadDir(filepath.Join(root, "projects"))
	if err != nil || len(got) != 2 || got[0].Name != "alice" || got[1].Name != "zed" || !reflect.DeepEqual(before, after) {
		t.Fatalf("list: %v %v", got, err)
	}
}

func TestListRejectsCorruptionAndForeignBindings(t *testing.T) {
	for _, mutation := range []string{"domain", "key", "corrupt", "symlink", "unexpected"} {
		t.Run(mutation, func(t *testing.T) {
			root := privateRoot(t)
			r := fixtureRecord()
			if err := Create(root, "work", r); err != nil {
				t.Fatal(err)
			}
			path := recordPath(root)
			var fixtureErr error
			switch mutation {
			case "domain":
				r.Domain = "personal"
				fixtureErr = os.WriteFile(path, mustJSON(t, r), 0600)
			case "key":
				r.Name = "other"
				fixtureErr = os.WriteFile(path, mustJSON(t, r), 0600)
			case "corrupt":
				fixtureErr = os.WriteFile(path, []byte("{}"), 0600)
			case "symlink":
				if err := os.Rename(path, path+".outside"); err != nil {
					t.Fatal(err)
				}
				fixtureErr = os.Symlink(path+".outside", path)
			case "unexpected":
				fixtureErr = os.WriteFile(filepath.Join(root, "projects", "unexpected"), []byte("data"), 0600)
			}
			if fixtureErr != nil {
				t.Fatal(fixtureErr)
			}
			if _, err := List(root, "work"); err == nil {
				t.Fatal("invalid registry admitted")
			}
		})
	}
}

func TestListBoundsRegistryBeforeReadingRecords(t *testing.T) {
	root := privateRoot(t)
	if err := os.Mkdir(filepath.Join(root, "projects"), 0700); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 1025; i++ {
		if err := os.WriteFile(filepath.Join(root, "projects", fmt.Sprintf("dev%d.json", i)), []byte("{}"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := List(root, "work"); err == nil || !strings.Contains(err.Error(), "exceeds 1024") {
		t.Fatalf("unbounded registry: %v", err)
	}
}
