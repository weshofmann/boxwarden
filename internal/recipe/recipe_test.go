package recipe

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const validRecipe = `{
  "version": 1,
  "source": {
    "kind": "ubuntu-24.04.4-desktop-arm64",
    "sha256": "c2610520bf582976839a1724c669e1cfed0547427be5a0ad12d457b92b46ffbe"
  },
  "machine": {"cpus": 4, "memory_mib": 4096, "system_disk_gib": 30},
  "apt_packages": ["git", "nodejs"],
  "steps": [
    {"id": "install-chatgpt", "phase": "once", "argv": ["/bin/bash", "-ec", "echo guest-only"]}
  ],
  "workspaces": [
    {"name": "project", "mount": "/home/boxwarden/workspaces/project"}
  ],
  "launch": [
    {"id": "chatgpt", "argv": ["/usr/bin/chatgpt"]}
  ]
}`

func writeRecipe(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "recipe.json")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadSupportedRecipeKeepsGuestOperationsExplicit(t *testing.T) {
	got, err := Load(writeRecipe(t, validRecipe))
	if err != nil {
		t.Fatal(err)
	}
	if got.Source.Kind != "ubuntu-24.04.4-desktop-arm64" || got.Machine.MemoryMiB != 4096 {
		t.Fatalf("source or machine lost: %+v", got)
	}
	if len(got.AptPackages) != 2 || got.AptPackages[0] != "git" || got.AptPackages[1] != "nodejs" {
		t.Fatalf("package order changed: %#v", got.AptPackages)
	}
	if len(got.Steps) != 1 || got.Steps[0].Phase != "once" || got.Steps[0].Argv[2] != "echo guest-only" {
		t.Fatalf("guest step changed: %#v", got.Steps)
	}
	if len(got.Workspaces) != 1 || got.Workspaces[0].Mount != "/home/boxwarden/workspaces/project" {
		t.Fatalf("workspace mount changed: %#v", got.Workspaces)
	}
	if len(got.Launch) != 1 || got.Launch[0].Argv[0] != "/usr/bin/chatgpt" {
		t.Fatalf("launch intent changed: %#v", got.Launch)
	}
}

func TestLoadRejectsAmbiguousOrUnsupportedRecipe(t *testing.T) {
	for name, input := range map[string]string{
		"duplicate-field": strings.Replace(validRecipe, `"version": 1,`, `"version": 1, "version": 2,`, 1),
		"unknown-field":   strings.Replace(validRecipe, `"version": 1,`, `"version": 1, "host_command": "rm -rf /",`, 1),
		"wrong-version":   strings.Replace(validRecipe, `"version": 1,`, `"version": 2,`, 1),
		"wrong-source":    strings.Replace(validRecipe, `ubuntu-24.04.4-desktop-arm64`, `arbitrary-image`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Load(writeRecipe(t, input)); err == nil {
				t.Fatal("unsafe or unsupported recipe was accepted")
			}
		})
	}
}

func TestLoadRejectsHostFacingGuestOperations(t *testing.T) {
	for name, input := range map[string]string{
		"package-shell":   strings.Replace(validRecipe, `"git"`, `"git;reboot"`, 1),
		"empty-step":      strings.Replace(validRecipe, `"/bin/bash", "-ec", "echo guest-only"`, ``, 1),
		"host-mount":      strings.Replace(validRecipe, `/home/boxwarden/workspaces/project`, `/Users/devel/project`, 1),
		"mount-traversal": strings.Replace(validRecipe, `/home/boxwarden/workspaces/project`, `/home/boxwarden/workspaces/../project`, 1),
		"control-byte":    strings.Replace(validRecipe, `echo guest-only`, `echo \u0000 guest-only`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Load(writeRecipe(t, input)); err == nil {
				t.Fatal("unsafe guest operation was accepted")
			}
		})
	}
}

func TestVerifyISORejectsWrongBytesAndNonRegularSource(t *testing.T) {
	root := t.TempDir()
	wrong := filepath.Join(root, "wrong.iso")
	if err := os.WriteFile(wrong, []byte("not the Canonical image"), 0o600); err != nil {
		t.Fatal(err)
	}
	for name, source := range map[string]string{
		"wrong-bytes": wrong,
		"directory":   root,
		"symlink":     filepath.Join(root, "link.iso"),
	} {
		if name == "symlink" {
			if err := os.Symlink(wrong, source); err != nil {
				t.Fatal(err)
			}
		}
		t.Run(name, func(t *testing.T) {
			if err := VerifyISO(source); err == nil {
				t.Fatal("unverified installer was accepted")
			}
		})
	}
}

func TestVerifyDigestChecksExactRegularFile(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "input.iso")
	if err := os.WriteFile(filename, []byte("known input"), 0o600); err != nil {
		t.Fatal(err)
	}
	const digest = "6fdccd8e1aa8ecede3d631c3d6182eeaefece8235805d1dc1461ee3a1e52183b"
	if err := verifyDigest(filename, digest, int64(len("known input"))); err != nil {
		t.Fatal(err)
	}
	if err := verifyDigest(filename, digest, int64(len("known input")+1)); err == nil {
		t.Fatal("wrong size was accepted")
	}
}
