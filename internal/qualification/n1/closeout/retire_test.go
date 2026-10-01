package closeout

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestRetireExactStateAndPreserveSibling(t *testing.T) {
	s, h := inventoryFixture(t)
	sibling := s.root + "-sibling"
	if e := os.Mkdir(sibling, 0700); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { os.Remove(sibling) })
	g, e := inventory(t.Context(), s, h)
	if e != nil {
		t.Fatal(e)
	}
	if e = g.Retire(t.Context()); e != nil {
		t.Fatal(e)
	}
	if _, e = os.Lstat(s.root); !os.IsNotExist(e) {
		t.Fatal("owned root not absent", e)
	}
	if _, e = os.Lstat(sibling); e != nil {
		t.Fatal("sibling changed", e)
	}
	if e = g.Close(); e != nil {
		t.Fatal(e)
	}
}
func TestRetireRefusesDriftBeforeMutation(t *testing.T) {
	for _, mode := range []string{"unknown", "changed", "renamed", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			s, h := inventoryFixture(t)
			g, e := inventory(t.Context(), s, h)
			if e != nil {
				t.Fatal(e)
			}
			defer g.Close()
			ctx := context.Background()
			switch mode {
			case "unknown":
				if e = os.WriteFile(filepath.Join(s.root, "sessions/.unknown"), []byte("preserve"), 0600); e != nil {
					t.Fatal(e)
				}
			case "changed":
				if e = os.WriteFile(filepath.Join(s.root, "identity/ssh-user-ca/ca"), []byte("changed private bytes"), 0600); e != nil {
					t.Fatal(e)
				}
			case "renamed":
				if e = os.Rename(s.root, s.root+"-retained"); e != nil {
					t.Fatal(e)
				}
				if e = os.Mkdir(s.root, 0700); e != nil {
					t.Fatal(e)
				}
			case "cancelled":
				c, cancel := context.WithCancel(ctx)
				cancel()
				ctx = c
			}
			if e = g.Retire(ctx); e == nil {
				t.Fatal("uncertain state retired")
			}
			r := s.root
			if mode == "renamed" {
				r += "-retained"
			}
			if _, e = os.Lstat(filepath.Join(r, "identity/ssh-user-ca/ca")); e != nil {
				t.Fatal("private metadata leaf mutated despite failed preflight", e)
			}
		})
	}
}

func TestRetireRefusesChangedParentBeforeAnyRemoval(t *testing.T) {
	s, h := inventoryFixture(t)
	g, e := inventory(t.Context(), s, h)
	if e != nil {
		t.Fatal(e)
	}
	defer g.Close()
	p := filepath.Dir(s.root)
	old, e := os.Stat(p)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.Chmod(p, 0777); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { os.Chmod(p, old.Mode().Perm()) })
	if e = g.Retire(t.Context()); e == nil {
		t.Fatal("unsafe changed parent retired state")
	}
	if _, e = os.Lstat(filepath.Join(s.root, "identity/ssh-user-ca/ca")); e != nil {
		t.Fatal("mutation before parent proof", e)
	}
}
