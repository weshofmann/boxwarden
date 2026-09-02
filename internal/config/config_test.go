package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadResolvesOnlyTheNamedDomain(t *testing.T) {
	base := canonicalTempDir(t)
	workRoot := makeRoot(t, base, "work")
	personalRoot := makeRoot(t, base, "personal")
	path := writeConfig(t, base, fmt.Sprintf(`{"version":1,"domains":{"work":{"state_root":%q},"personal":{"state_root":%q}}}`, workRoot, personalRoot))

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	work, err := loaded.Domain("work")
	if err != nil {
		t.Fatalf("Domain(work) error = %v", err)
	}
	if got, want := work.StateRoot, workRoot; got != want {
		t.Fatalf("Domain(work).StateRoot = %q, want %q", got, want)
	}

	personal, err := loaded.Domain("personal")
	if err != nil {
		t.Fatalf("Domain(personal) error = %v", err)
	}
	if got, want := personal.StateRoot, personalRoot; got != want {
		t.Fatalf("Domain(personal).StateRoot = %q, want %q", got, want)
	}

	if _, err := loaded.Domain("unknown"); err == nil {
		t.Fatal("Domain(unknown) error = nil, want unknown-domain error")
	}
}

func TestLoadVersion2RequiresAndExposesHostPrerequisites(t *testing.T) {
	base := canonicalTempDir(t)
	root := makeRoot(t, base, "work")
	path := writeConfig(t, base, fmt.Sprintf(`{"version":2,"host":{"tart_path":"/opt/qualified/tart","tart_home":"%s/tart-home","softnet_path":"/opt/qualified/softnet"},"domains":{"work":{"state_root":%q}}}`, base, root))

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	host, err := loaded.Host()
	if err != nil {
		t.Fatalf("Host() error = %v", err)
	}
	if host.TartPath != "/opt/qualified/tart" || host.SoftnetPath != "/opt/qualified/softnet" {
		t.Fatalf("Host() = %#v, want configured prerequisite paths", host)
	}

	missing := writeConfig(t, base, fmt.Sprintf(`{"version":2,"domains":{"work":{"state_root":%q}}}`, root))
	if _, err := Load(missing); err == nil {
		t.Fatal("Load(version 2 without host) error = nil, want rejection")
	}

	legacy := writeConfig(t, base, fmt.Sprintf(`{"version":1,"domains":{"work":{"state_root":%q}}}`, root))
	loaded, err = Load(legacy)
	if err != nil {
		t.Fatalf("Load(version 1) error = %v", err)
	}
	if _, err := loaded.Host(); err == nil {
		t.Fatal("Host(version 1) error = nil, want V3 prerequisite gate")
	}
}

func TestLoadRejectsAmbiguousOrUnsafeConfiguration(t *testing.T) {
	base := canonicalTempDir(t)
	workRoot := makeRoot(t, base, "work")
	personalRoot := makeRoot(t, base, "personal")
	overlap := filepath.Join(workRoot, "nested")
	if err := os.Mkdir(overlap, 0o700); err != nil {
		t.Fatalf("Mkdir overlap: %v", err)
	}
	symlinkRoot := filepath.Join(base, "symlink-root")
	if err := os.Symlink(workRoot, symlinkRoot); err != nil {
		t.Fatalf("Symlink: %v", err)
	}

	for name, contents := range map[string]string{
		"unknown top-level field": fmt.Sprintf(`{"version":1,"domains":{"work":{"state_root":%q}},"extra":true}`, workRoot),
		"unknown domain field":    fmt.Sprintf(`{"version":1,"domains":{"work":{"state_root":%q,"extra":true}}}`, workRoot),
		"wrong version":           fmt.Sprintf(`{"version":2,"domains":{"work":{"state_root":%q}}}`, workRoot),
		"invalid domain id":       fmt.Sprintf(`{"version":1,"domains":{"Work":{"state_root":%q}}}`, workRoot),
		"duplicate domain id":     fmt.Sprintf(`{"version":1,"domains":{"work":{"state_root":%q},"work":{"state_root":%q}}}`, workRoot, personalRoot),
		"relative root":           `{"version":1,"domains":{"work":{"state_root":"relative"}}}`,
		"missing root":            fmt.Sprintf(`{"version":1,"domains":{"work":{"state_root":%q}}}`, filepath.Join(base, "missing")),
		"symlink root":            fmt.Sprintf(`{"version":1,"domains":{"work":{"state_root":%q}}}`, symlinkRoot),
		"overlapping roots":       fmt.Sprintf(`{"version":1,"domains":{"work":{"state_root":%q},"personal":{"state_root":%q}}}`, workRoot, overlap),
	} {
		t.Run(name, func(t *testing.T) {
			path := writeConfig(t, base, contents)
			if _, err := Load(path); err == nil {
				t.Fatal("Load() error = nil, want rejection")
			}
		})
	}
}

func TestLoadRejectsSymlinkedConfigFile(t *testing.T) {
	base := canonicalTempDir(t)
	workRoot := makeRoot(t, base, "work")
	target := writeConfig(t, base, fmt.Sprintf(`{"version":1,"domains":{"work":{"state_root":%q}}}`, workRoot))
	link := filepath.Join(base, "config-link.json")
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("Symlink: %v", err)
	}

	if _, err := Load(link); err == nil {
		t.Fatal("Load(symlink) error = nil, want rejection")
	}
}

func TestLoadRejectsAOneCharacterNestedDomainRoot(t *testing.T) {
	base := canonicalTempDir(t)
	parent := makeRoot(t, base, "a")
	child := makeRoot(t, parent, "b")
	path := writeConfig(t, base, fmt.Sprintf(`{"version":1,"domains":{"work":{"state_root":%q},"personal":{"state_root":%q}}}`, parent, child))

	if _, err := Load(path); err == nil {
		t.Fatal("Load() error = nil, want overlapping roots rejection")
	}
}

func TestLoadRejectsCaseAliasedPhysicalDomainRoot(t *testing.T) {
	base, err := os.MkdirTemp("/private/tmp", "boxwarden-config-case-")
	if err != nil {
		t.Skipf("qualified macOS temporary root unavailable: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(base) })
	base, err = filepath.EvalSymlinks(base)
	if err != nil {
		t.Fatal(err)
	}
	workRoot := makeRoot(t, base, "work")
	alias := strings.Replace(workRoot, "/private/tmp/", "/PRIVATE/TMP/", 1)
	if alias == workRoot {
		t.Skip("test requires the qualified macOS /private/tmp path")
	}
	originalInfo, err := os.Stat(workRoot)
	if err != nil {
		t.Fatal(err)
	}
	aliasInfo, err := os.Stat(alias)
	if err != nil || !os.SameFile(originalInfo, aliasInfo) {
		t.Skip("filesystem is not case-insensitive for this path")
	}
	path := writeConfig(t, base, fmt.Sprintf(`{"version":1,"domains":{"work":{"state_root":%q},"personal":{"state_root":%q}}}`, workRoot, alias))

	if _, err := Load(path); err == nil {
		t.Fatal("Load() error = nil, want physically identical case-aliased roots rejected")
	}
}

func TestLoadRejectsGroupAccessibleDomainRoot(t *testing.T) {
	base := canonicalTempDir(t)
	workRoot := makeRoot(t, base, "work")
	if err := os.Chmod(workRoot, 0o750); err != nil {
		t.Fatal(err)
	}
	path := writeConfig(t, base, fmt.Sprintf(`{"version":1,"domains":{"work":{"state_root":%q}}}`, workRoot))

	if _, err := Load(path); err == nil {
		t.Fatal("Load() error = nil, want group-accessible root rejection")
	}
}

func TestLoadRejectsDomainRootWithoutExactOwnerPrivateMode(t *testing.T) {
	base := canonicalTempDir(t)
	workRoot := makeRoot(t, base, "work")
	if err := os.Chmod(workRoot, 0o500); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(workRoot, 0o700)
	path := writeConfig(t, base, fmt.Sprintf(`{"version":1,"domains":{"work":{"state_root":%q}}}`, workRoot))

	if _, err := Load(path); err == nil {
		t.Fatal("Load() error = nil, want non-writable state root rejection")
	}
}

func makeRoot(t *testing.T, base, name string) string {
	t.Helper()
	path := filepath.Join(base, name)
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatalf("Mkdir(%q): %v", path, err)
	}
	return path
}

func writeConfig(t *testing.T, base, contents string) string {
	t.Helper()
	path := filepath.Join(base, strings.ReplaceAll(t.Name(), "/", "_")+".json")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("WriteFile(%q): %v", path, err)
	}
	return path
}

func canonicalTempDir(t *testing.T) string {
	t.Helper()
	path, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("EvalSymlinks temp dir: %v", err)
	}
	return path
}
