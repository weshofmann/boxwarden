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
