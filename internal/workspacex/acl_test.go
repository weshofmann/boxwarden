package workspacex

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/weshofmann/boxwarden/internal/domain"
)

type testACLInspector struct {
	acl  map[string]bool
	fail map[string]bool
}

type aclInspectorFunc func(string) (bool, error)

func (f aclInspectorFunc) HasExtendedACL(path string) (bool, error) { return f(path) }

func (i testACLInspector) HasExtendedACL(path string) (bool, error) {
	if i.fail[path] {
		return false, errors.New("ACL inspection failed")
	}
	return i.acl[path], nil
}

func TestMain(m *testing.M) {
	aclInspector = testACLInspector{}
	os.Exit(m.Run())
}

func TestWorkspaceRecordAdmissionRejectsACLAndInspectionError(t *testing.T) {
	for _, kind := range []string{"root ACL", "directory ACL", "record ACL", "record inspection error"} {
		t.Run(kind, func(t *testing.T) {
			root := privateRoot(t)
			record := fixtureRecord()
			if err := SaveRecord(root, domain.ID("work"), record); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(root, "workspaces", testVolumeID+".json")
			if kind == "root ACL" {
				path = root
			} else if kind == "directory ACL" {
				path = filepath.Join(root, "workspaces")
			}
			inspector := testACLInspector{acl: map[string]bool{}, fail: map[string]bool{}}
			if kind == "record inspection error" {
				inspector.fail[path] = true
			} else {
				inspector.acl[path] = true
			}
			aclInspector = inspector
			t.Cleanup(func() { aclInspector = testACLInspector{} })
			if _, err := LoadRecord(root, domain.ID("work"), testVolumeID); err == nil {
				t.Fatalf("%s accepted", kind)
			}
		})
	}
}

func TestWorkspaceDiskAdmissionRejectsFileACL(t *testing.T) {
	root := privateRoot(t)
	volumeDir := filepath.Join(root, "volumes")
	if err := os.Mkdir(volumeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(volumeDir, testVolumeID+".raw")
	if err := os.WriteFile(path, make([]byte, 4096), 0o600); err != nil {
		t.Fatal(err)
	}
	aclInspector = testACLInspector{acl: map[string]bool{path: true}}
	t.Cleanup(func() { aclInspector = testACLInspector{} })
	if _, err := InspectManagedDisk(root, testVolumeID, 4096); err == nil {
		t.Fatal("raw file with extended ACL admitted")
	}
}

func TestSaveRecordRejectsACLOnNewTemporaryRecord(t *testing.T) {
	root := privateRoot(t)
	aclInspector = aclInspectorFunc(func(path string) (bool, error) {
		return strings.Contains(path, ".json.tmp-"), nil
	})
	t.Cleanup(func() { aclInspector = testACLInspector{} })
	if err := SaveRecord(root, domain.ID("work"), fixtureRecord()); err == nil {
		t.Fatal("new record with extended ACL accepted")
	}
	if _, err := os.Lstat(filepath.Join(root, "workspaces", testVolumeID+".json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("record published despite ACL rejection: %v", err)
	}
}
