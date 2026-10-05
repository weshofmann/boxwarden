package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/weshofmann/boxwarden/internal/app"
)

// Omitting the production adapter leaves native preparation as a no-op despite
// a valid structured route. Unsafe package input must fail before side effects.
func TestPublicSetupPrepareRejectsUnsafePackageBeforeEffects(t *testing.T) {
	o := publicOptions(&bytes.Buffer{})
	if o.SetupPrepare == nil {
		t.Fatal("production package preparation adapter is missing")
	}
	for _, root := range []string{"relative", "/missing-package"} {
		_, uncertain, err := o.SetupPrepare(context.Background(), "/config", app.SetupPrepareInput{PackageRoot: root}, &bytes.Buffer{})
		if err == nil || uncertain {
			t.Fatalf("unsafe package %q: uncertain=%v err=%v", root, uncertain, err)
		}
	}
}

func setupPackageFixture(t *testing.T) (string, string, string) {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(root, "support", "source")
	if err := os.MkdirAll(filepath.Join(source, "tools", "private-beta"), 0700); err != nil {
		t.Fatal(err)
	}
	script := []byte("#!/bin/bash\nexit 0\n")
	for _, path := range []string{filepath.Join(root, "prepare-projects.sh"), filepath.Join(source, "tools", "private-beta", "prepare-projects.sh")} {
		if err := os.WriteFile(path, script, 0600); err != nil {
			t.Fatal(err)
		}
	}
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("/usr/bin/git", append([]string{"-C", source}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git("init")
	git("add", ".")
	git("-c", "user.name=synthetic", "-c", "user.email=synthetic@example.invalid", "-c", "commit.gpgsign=false", "commit", "-m", "synthetic fixture")
	revision := git("rev-parse", "HEAD")
	if err := os.Mkdir(filepath.Join(root, "bin"), 0700); err != nil {
		t.Fatal(err)
	}
	executable := filepath.Join(root, "current-cli")
	for _, path := range []string{executable, filepath.Join(root, "bin", "boxwarden")} {
		if err := os.WriteFile(path, []byte("synthetic CLI bytes"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	return root, executable, revision
}

// Changing the source/script/binary pairing must invalidate the admission;
// possession of an executable package path alone cannot authorize shell code.
func TestSetupPackageAdmissionBindsSourceScriptAndCLI(t *testing.T) {
	root, executable, revision := setupPackageFixture(t)
	if err := admitSetupPackage(t.Context(), root, executable, revision); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name     string
		alter    func()
		revision string
	}{
		{"revision mismatch", func() {}, "0000000000000000000000000000000000000000"},
		{"different CLI", func() {
			if err := os.WriteFile(filepath.Join(root, "bin", "boxwarden"), []byte("other CLI"), 0700); err != nil {
				t.Fatal(err)
			}
		}, revision},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			tt.alter()
			if err := admitSetupPackage(t.Context(), root, executable, tt.revision); err == nil {
				t.Fatal("admitted mismatched package")
			}
		})
	}
}

func TestSetupPackageRejectsMutablePreparationScript(t *testing.T) {
	root, executable, revision := setupPackageFixture(t)
	if err := os.Chmod(filepath.Join(root, "prepare-projects.sh"), 0666); err != nil {
		t.Fatal(err)
	}
	if err := admitSetupPackage(t.Context(), root, executable, revision); err == nil {
		t.Fatal("admitted externally writable helper")
	}
}

func TestSetupPackageRejectsChangedScriptAndSymlinkSource(t *testing.T) {
	for _, kind := range []string{"changed script", "symlink source"} {
		t.Run(kind, func(t *testing.T) {
			root, executable, revision := setupPackageFixture(t)
			if kind == "changed script" {
				if err := os.WriteFile(filepath.Join(root, "prepare-projects.sh"), []byte("#!/bin/bash\necho changed\n"), 0600); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.Symlink("/bin/bash", filepath.Join(root, "support", "source", "link")); err != nil {
					t.Fatal(err)
				}
			}
			if err := admitSetupPackage(t.Context(), root, executable, revision); err == nil {
				t.Fatal("admitted changed helper/source")
			}
		})
	}
}

func TestSetupPackageRejectsSymlinkedSupportAncestor(t *testing.T) {
	root, executable, revision := setupPackageFixture(t)
	support := filepath.Join(root, "support")
	moved := filepath.Join(root, "other-support")
	if err := os.Rename(support, moved); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(moved, support); err != nil {
		t.Fatal(err)
	}
	if err := admitSetupPackage(t.Context(), root, executable, revision); err == nil {
		t.Fatal("admitted symlinked source ancestor")
	}
}

func TestSetupPackageRejectsOversizedCLIBeforeHashing(t *testing.T) {
	root, executable, revision := setupPackageFixture(t)
	file, err := os.OpenFile(filepath.Join(root, "bin", "boxwarden"), os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(129 << 20); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := admitSetupPackage(t.Context(), root, executable, revision); err == nil || !strings.Contains(err.Error(), "size limit") {
		t.Fatalf("oversized CLI admission: %v", err)
	}
}
