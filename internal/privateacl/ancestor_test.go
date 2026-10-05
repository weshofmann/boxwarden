package privateacl

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

type ancestorInspectorFunc func(string) (bool, error)

func (f ancestorInspectorFunc) HasUnsafeAncestorACL(path string) (bool, error) { return f(path) }

func TestAncestorACLClassifierOnlyAcceptsExactAppleDeleteDenial(t *testing.T) {
	for _, text := range []string{"drwx------ 2 owner staff 64 date /path\n", "drwx------+ 2 owner staff 64 date /path\n 0: group:everyone deny delete\n", "drwx------@ 2 owner staff 64 date /path\n 0: group:everyone deny delete\n"} {
		unsafe, err := unsafeAncestorACL(text)
		if err != nil || unsafe {
			t.Fatalf("safe=%q: %v,%v", text, unsafe, err)
		}
	}
	for _, acl := range []string{"0: group:everyone allow delete", "0: user:everyone deny delete", "0: group:everyone deny delete,write", "0: group:everyone inherited deny delete", "0: group:everyone deny delete\n1: user:other allow write", "", "0: group:everyone deny delete_child"} {
		unsafe, err := unsafeAncestorACL("drwx------+ 2 owner staff 64 date /path\n" + acl + "\n")
		if err == nil && !unsafe {
			t.Fatalf("unsafe admitted: %q", acl)
		}
	}
}

func TestSafeAncestorCheckIdentityBracketsInspection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ancestor")
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Lstat(path)
	if err := CheckSafeAncestor(path, info, ancestorInspectorFunc(func(string) (bool, error) { return false, nil })); err != nil {
		t.Fatal(err)
	}
	for _, inspector := range []AncestorInspector{ancestorInspectorFunc(func(string) (bool, error) { return true, nil }), ancestorInspectorFunc(func(string) (bool, error) { return false, errors.New("inspection unavailable") }), ancestorInspectorFunc(func(string) (bool, error) { return false, os.Chmod(path, 0775) })} {
		if err := CheckSafeAncestor(path, info, inspector); err == nil {
			t.Fatal("unsafe or changed ancestor admitted")
		}
	}
}
