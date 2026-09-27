package workspaceformat

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
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

func TestFormatAdmissionRejectsPrivatePathACL(t *testing.T) {
	for _, kind := range []string{"root ACL", "volumes ACL", "raw ACL", "journal inspection error"} {
		t.Run(kind, func(t *testing.T) {
			root := testRoot(t)
			if _, err := Create(t.Context(), root, testRequest(), successfulFormatter(t)); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(root, "volumes", testVolumeID+".format.json")
			switch kind {
			case "root ACL":
				path = root
			case "volumes ACL":
				path = filepath.Join(root, "volumes")
			case "raw ACL":
				path = filepath.Join(root, "volumes", testVolumeID+".raw")
			}
			inspector := testACLInspector{acl: map[string]bool{}, fail: map[string]bool{}}
			if kind == "journal inspection error" {
				inspector.fail[path] = true
			} else {
				inspector.acl[path] = true
			}
			aclInspector = inspector
			t.Cleanup(func() { aclInspector = testACLInspector{} })
			if file, _, err := Admit(root, testRequest()); err == nil {
				file.Close()
				t.Fatalf("%s accepted", kind)
			}
		})
	}
}

func TestCreateRejectsACLBeforeFormatterRuns(t *testing.T) {
	for _, kind := range []string{"root ACL", "root inspection error", "volumes ACL", "initial journal ACL"} {
		t.Run(kind, func(t *testing.T) {
			root := testRoot(t)
			if kind == "volumes ACL" {
				if err := os.Mkdir(filepath.Join(root, "volumes"), 0o700); err != nil {
					t.Fatal(err)
				}
			}
			aclInspector = aclInspectorFunc(func(path string) (bool, error) {
				switch kind {
				case "root ACL":
					return path == root, nil
				case "root inspection error":
					if path == root {
						return false, errors.New("ACL inspection failed")
					}
				case "volumes ACL":
					return path == filepath.Join(root, "volumes"), nil
				case "initial journal ACL":
					return strings.HasSuffix(path, ".format.json"), nil
				}
				return false, nil
			})
			t.Cleanup(func() { aclInspector = testACLInspector{} })
			called := false
			formatter := formatFunc(func(_ context.Context, request FormatRequest) (FormatEvidence, error) {
				called = true
				return FormatEvidence{}, nil
			})
			if _, err := Create(t.Context(), root, testRequest(), formatter); err == nil || called {
				t.Fatalf("%s reached formatter: err=%v called=%v", kind, err, called)
			}
		})
	}
}
