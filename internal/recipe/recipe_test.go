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

func TestTrackedChatGPTRecipeUsesPinnedGuestPreparation(t *testing.T) {
	path := filepath.Join("..", "..", "examples", "v0.2-alpha-chatgpt.json")
	recipe, err := LoadRunnable(path)
	if err != nil {
		t.Fatalf("tracked ChatGPT recipe is not runnable: %v", err)
	}
	if len(recipe.Steps) != 2 || recipe.Steps[0].Phase != "prepare" ||
		len(recipe.Steps[0].Argv) != 1 || recipe.Steps[0].Argv[0] != "/usr/local/libexec/boxwarden-install-pinned-chatgpt" {
		t.Fatalf("tracked ChatGPT preparation changed: %+v", recipe.Steps)
	}
	if recipe.Steps[1].Phase != "startup" || len(recipe.Steps[1].Argv) != 1 ||
		recipe.Steps[1].Argv[0] != "/usr/local/libexec/boxwarden-launch-chatgpt" {
		t.Fatalf("tracked ChatGPT startup changed: %+v", recipe.Steps)
	}
}

func TestTrackedAutomaticActionRecipeHasOrderedSyntheticSteps(t *testing.T) {
	path := filepath.Join("..", "..", "examples", "v0.2-alpha-actions.json")
	recipe, err := LoadRunnable(path)
	if err != nil {
		t.Fatalf("tracked action recipe is not runnable: %v", err)
	}
	if len(recipe.Steps) != 2 || recipe.Steps[0].Phase != "once" ||
		recipe.Steps[1].Phase != "startup" {
		t.Fatalf("tracked action recipe lost ordered phases: %+v", recipe.Steps)
	}
}

func TestCanonicalIntentSeparatesSessionActionsFromReusableBase(t *testing.T) {
	path := writeRecipe(t, validRecipe)
	before, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	first, firstDigest, err := CanonicalIntent(before)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	changedSource := strings.Replace(validRecipe, "echo guest-only", "echo changed", 1)
	if err := os.WriteFile(path, []byte(changedSource), 0o600); err != nil {
		t.Fatal(err)
	}
	oldAgain, oldDigest, err := CanonicalIntent(before)
	if err != nil || oldDigest != firstDigest || string(oldAgain) != string(first) {
		t.Fatalf("captured intent changed after source mutation: digest=%q err=%v", oldDigest, err)
	}
	after, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	_, secondDigest, err := CanonicalIntent(after)
	if err != nil || secondDigest == firstDigest {
		t.Fatalf("changed session action did not change intent identity: digest=%q err=%v", secondDigest, err)
	}
	const definitionSHA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	firstBase, err := PreparationKey(before, definitionSHA)
	if err != nil {
		t.Fatal(err)
	}
	secondBase, err := PreparationKey(after, definitionSHA)
	if err != nil || firstBase != secondBase {
		t.Fatalf("session action changed reusable base identity: before=%q after=%q err=%v", firstBase, secondBase, err)
	}
	compact, err := Load(writeRecipe(t, strings.ReplaceAll(validRecipe, "\n", "")))
	if err != nil {
		t.Fatal(err)
	}
	_, compactDigest, err := CanonicalIntent(compact)
	if err != nil || compactDigest != firstDigest {
		t.Fatalf("formatting changed canonical intent: digest=%q err=%v", compactDigest, err)
	}
}

func TestDecodeIntentAdmitsOnlyCanonicalValidatedSnapshot(t *testing.T) {
	value, err := Load(writeRecipe(t, validRecipe))
	if err != nil {
		t.Fatal(err)
	}
	raw, _, err := CanonicalIntent(value)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeIntent(raw)
	if err != nil || len(decoded.Steps) != 1 || decoded.Steps[0].Phase != "once" {
		t.Fatalf("decoded canonical intent = %#v, %v", decoded, err)
	}
	for name, altered := range map[string][]byte{
		"whitespace":         append(append([]byte(nil), raw...), '\n'),
		"unknown envelope":   []byte(strings.Replace(string(raw), `"intent_version":1`, `"intent_version":1,"extra":true`, 1)),
		"duplicate envelope": []byte(strings.Replace(string(raw), `"intent_version":1`, `"intent_version":1,"intent_version":1`, 1)),
		"wrong version":      []byte(strings.Replace(string(raw), `"intent_version":1`, `"intent_version":2`, 1)),
		"invalid recipe":     []byte(strings.Replace(string(raw), `"version":1`, `"version":2`, 1)),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := DecodeIntent(altered); err == nil {
				t.Fatal("noncanonical or invalid intent accepted")
			}
		})
	}
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

func TestLoadRunnableAdmitsGuestActionsWithImplementedLifecycles(t *testing.T) {
	prepareOnly := strings.Replace(validRecipe, `"phase": "once"`, `"phase": "prepare"`, 1)
	prepareOnly = strings.Replace(prepareOnly, `"launch": [
    {"id": "chatgpt", "argv": ["/usr/bin/chatgpt"]}
  ]`, `"launch": []`, 1)
	if _, err := LoadRunnable(writeRecipe(t, prepareOnly)); err != nil {
		t.Fatalf("prepare-only recipe rejected: %v", err)
	}
	reconfigure := strings.Replace(prepareOnly, `"phase": "prepare"`, `"phase": "reconfigure"`, 1)
	if got, err := LoadRunnable(writeRecipe(t, reconfigure)); err != nil || len(got.Steps) != 1 || got.Steps[0].Phase != "reconfigure" {
		t.Fatalf("explicit reconfigure recipe was not admitted: steps=%+v err=%v", got.Steps, err)
	}
	for name, input := range map[string]string{
		"once":    strings.Replace(prepareOnly, `"phase": "prepare"`, `"phase": "once"`, 1),
		"startup": strings.Replace(prepareOnly, `"phase": "prepare"`, `"phase": "startup"`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			if got, err := LoadRunnable(writeRecipe(t, input)); err != nil || len(got.Steps) != 1 || got.Steps[0].Phase != name {
				t.Fatalf("supported %s action rejected: %+v, %v", name, got.Steps, err)
			}
		})
	}
	launch := strings.Replace(prepareOnly, `"launch": []`, `"launch": [{"id":"chatgpt","argv":["/usr/bin/chatgpt"]}]`, 1)
	if _, err := LoadRunnable(writeRecipe(t, launch)); err == nil || !strings.Contains(err.Error(), "launch") {
		t.Fatalf("unsupported launch action accepted or unreported: %v", err)
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
