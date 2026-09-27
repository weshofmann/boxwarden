package privateacl

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

type inspectorFunc func(string) (bool, error)

func (f inspectorFunc) HasExtendedACL(path string) (bool, error) { return f(path) }

func TestCheckAcceptsStablePathWithoutExtendedACL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "private")
	if err := os.WriteFile(path, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := Check(path, info, inspectorFunc(func(got string) (bool, error) {
		if got != path {
			t.Fatalf("inspected %q, want %q", got, path)
		}
		return false, nil
	})); err != nil {
		t.Fatal(err)
	}
}

func TestCheckRejectsExtendedACLAndInspectionFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "private")
	if err := os.WriteFile(path, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, inspector := range []Inspector{
		inspectorFunc(func(string) (bool, error) { return true, nil }),
		inspectorFunc(func(string) (bool, error) { return false, errors.New("ls failed") }),
		nil,
	} {
		if err := Check(path, info, inspector); err == nil {
			t.Fatal("unverifiable or extended ACL accepted")
		}
	}
}

func TestCheckRejectsPathReplacementDuringACLInspection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "private")
	if err := os.WriteFile(path, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	inspector := inspectorFunc(func(string) (bool, error) {
		if err := os.Rename(path, path+".old"); err != nil {
			return false, err
		}
		return false, os.WriteFile(path, []byte("replacement"), 0o600)
	})
	if err := Check(path, info, inspector); err == nil {
		t.Fatal("path replacement during ACL inspection accepted")
	}
}

func TestCheckRejectsModeChangeOnSameInodeDuringACLInspection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "private")
	if err := os.WriteFile(path, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	inspector := inspectorFunc(func(string) (bool, error) {
		return false, os.Chmod(path, 0o644)
	})
	if err := Check(path, info, inspector); err == nil {
		t.Fatal("permission change on same inode accepted")
	}
}

func TestCheckRejectsSymlinkOrNoncanonicalPath(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	if err := os.WriteFile(target, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(link)
	if err != nil {
		t.Fatal(err)
	}
	clean := inspectorFunc(func(string) (bool, error) { return false, nil })
	if err := Check(link, info, clean); err == nil {
		t.Fatal("symlink accepted")
	}
	regular, err := os.Lstat(target)
	if err != nil {
		t.Fatal(err)
	}
	if err := Check(root+"/./target", regular, clean); err == nil {
		t.Fatal("noncanonical path accepted")
	}
}
