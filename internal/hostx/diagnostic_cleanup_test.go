//go:build n1diagnostic && n1cleanup && !n1candidate

package hostx

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestCleanupExactThreePreservesSiblingAndBlocksSH(t *testing.T) {
	p, m := diagnosticGuardFixture(t)
	parent := filepath.Dir(p.finalDir())
	sibling := filepath.Join(parent, "protected-sibling")
	if e := os.Mkdir(sibling, 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(sibling, "keep"), []byte("protected"), 0600); e != nil {
		t.Fatal(e)
	}
	g, e := acquireDiagnosticCleanup(t.Context(), p, m, nil)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = acquireDiagnosticTree(t.Context(), p, m, nil); !errors.Is(e, ErrDiagnosticLaunchBusy) {
		t.Fatalf("SH during cleanup: %v", e)
	}
	if e = g.RemoveExact(t.Context()); e != nil {
		t.Fatal(e)
	}
	if e = g.Close(); e != nil {
		t.Fatal(e)
	}
	if _, e = os.Lstat(p.finalDir()); !os.IsNotExist(e) {
		t.Fatalf("digest remains %v", e)
	}
	b, e := os.ReadFile(filepath.Join(sibling, "keep"))
	if e != nil || string(b) != "protected" {
		t.Fatal("sibling changed")
	}
	if _, e = acquireDiagnosticCleanup(t.Context(), p, m, nil); e == nil {
		t.Fatal("missing target adopted")
	}
}
func TestCleanupSHContentionAndDelayedDescriptorRefuse(t *testing.T) {
	p, m := diagnosticGuardFixture(t)
	sh, e := acquireDiagnosticTree(t.Context(), p, m, nil)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = acquireDiagnosticCleanup(t.Context(), p, m, nil); e == nil {
		t.Fatal("EX crossed SH")
	}
	if e = sh.Release(); e != nil {
		t.Fatal(e)
	}
	old, e := openNoFollow(filepath.Join(p.finalDir(), "launch.lock"))
	if e != nil {
		t.Fatal(e)
	}
	defer old.Close()
	g, e := acquireDiagnosticCleanup(t.Context(), p, m, nil)
	if e != nil {
		t.Fatal(e)
	}
	if e = g.RemoveExact(t.Context()); e != nil {
		t.Fatal(e)
	}
	if e = g.Close(); e != nil {
		t.Fatal(e)
	}
	if e = syscall.Flock(int(old.Fd()), syscall.LOCK_SH|syscall.LOCK_NB); e != nil {
		t.Fatal(e)
	}
	if _, e = acquireDiagnosticTree(t.Context(), p, m, nil); e == nil {
		t.Fatal("delayed descriptor became launch authority")
	}
}
func TestCleanupDriftAndEveryEffectFailureUnknown(t *testing.T) {
	for _, step := range []string{"before-softnet", "after-softnet", "before-manifest.json", "after-manifest.json", "before-launch.lock", "after-launch.lock", "before-directory", "after-directory", "before-fsync", "after-fsync", "close"} {
		t.Run(step, func(t *testing.T) {
			p, m := diagnosticGuardFixture(t)
			g, e := acquireDiagnosticCleanup(t.Context(), p, m, nil)
			if e != nil {
				t.Fatal(e)
			}
			g.fail = func(s string) error {
				if s == step {
					return errors.New("after-effect-control")
				}
				return nil
			}
			e = g.RemoveExact(t.Context())
			closeErr := g.Close()
			if e == nil && closeErr == nil {
				t.Fatal("failed return certified success")
			}
			if g.RemoveExact(t.Context()) == nil {
				t.Fatal("retry allowed")
			}
		})
	}
	for _, mutate := range []func(RootedPublisher){func(p RootedPublisher) { os.Chmod(filepath.Join(p.finalDir(), "launch.lock"), 0640) }, func(p RootedPublisher) { os.WriteFile(filepath.Join(p.finalDir(), ".stage-x"), nil, 0600) }, func(p RootedPublisher) { os.Rename(p.finalDir(), p.finalDir()+"old") }} {
		p, m := diagnosticGuardFixture(t)
		g, e := acquireDiagnosticCleanup(t.Context(), p, m, nil)
		if e != nil {
			t.Fatal(e)
		}
		mutate(p)
		if g.RemoveExact(t.Context()) == nil {
			t.Fatal("drift permitted effects")
		}
		g.Close()
	}
}

func TestCleanupPartialDriftStopsBeforeSecondRemoval(t *testing.T) {
	for _, kind := range []string{"parent-mode", "ancestor-mode", "parent-replace", "manifest-bytes"} {
		t.Run(kind, func(t *testing.T) {
			p, m := diagnosticGuardFixture(t)
			g, e := acquireDiagnosticCleanup(t.Context(), p, m, nil)
			if e != nil {
				t.Fatal(e)
			}
			parent := filepath.Dir(p.finalDir())
			manifest := filepath.Join(p.finalDir(), "manifest.json")
			g.fail = func(step string) error {
				if step != "after-softnet" {
					return nil
				}
				switch kind {
				case "parent-mode":
					if e := os.Chmod(parent, 0777); e != nil {
						t.Fatal(e)
					}
				case "ancestor-mode":
					if e := os.Chmod(filepath.Dir(parent), 0777); e != nil {
						t.Fatal(e)
					}
				case "parent-replace":
					if e := os.Rename(parent, parent+"-old"); e != nil {
						t.Fatal(e)
					}
					if e := os.Mkdir(parent, 0755); e != nil {
						t.Fatal(e)
					}
					manifest = filepath.Join(parent+"-old", filepath.Base(p.finalDir()), "manifest.json")
				case "manifest-bytes":
					if e := os.Chmod(manifest, 0644); e != nil {
						t.Fatal(e)
					}
					if e := os.WriteFile(manifest, []byte(`{"version":1,"altered":true}`), 0444); e != nil {
						t.Fatal(e)
					}
					if e := os.Chmod(manifest, 0444); e != nil {
						t.Fatal(e)
					}
				}
				return nil
			}
			if e := g.RemoveExact(t.Context()); e == nil {
				t.Fatal("partial drift accepted")
			}
			if _, e := os.Lstat(manifest); e != nil {
				t.Fatal("second removal crossed drift", e)
			}
			if g.RemoveExact(t.Context()) == nil {
				t.Fatal("retry allowed")
			}
			g.Close()
		})
	}
}
