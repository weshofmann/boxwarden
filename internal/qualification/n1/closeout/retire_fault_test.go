package closeout

import (
	"errors"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/contract"
	"os"
	"path/filepath"
	"testing"
)

func TestRetirementReturnErrorAfterEffectRemainsUnknown(t *testing.T) {
	s, h := inventoryFixture(t)
	removed := ""
	s.remove = func(r *os.Root, n string) error {
		e := r.Remove(n)
		if e == nil {
			removed = n
			return errors.New("unlink reported after effect")
		}
		return e
	}
	g, e := inventory(t.Context(), s, h)
	if e != nil {
		t.Fatal(e)
	}
	defer g.Close()
	if e = g.Retire(t.Context()); e == nil {
		t.Fatal("effect substituted for successful return")
	}
	if removed == "" {
		t.Fatal("aftereffect seam not exercised")
	}
	if _, e = os.Lstat(filepath.Join(s.root, removed)); !os.IsNotExist(e) {
		t.Fatal(e)
	}
	if _, e = os.Lstat(s.root); e != nil {
		t.Fatal("partial failure resumed retirement")
	}
}

type callbackACL func(string) (bool, error)

func (f callbackACL) HasExtendedACL(p string) (bool, error) { return f(p) }

func TestRetireExpiryDuringFinalACLDoesNotDispatchUnlink(t *testing.T) {
	s, h := inventoryFixture(t)
	expired, checks, removed := false, 0, 0
	target := filepath.Join(s.root, contract.PublicRecordNames[1])
	s.acl = callbackACL(func(p string) (bool, error) {
		if p == target {
			checks++
			if checks == 3 {
				expired = true
			}
		}
		return false, nil
	})
	s.check = func() error {
		if expired {
			return ErrRefused
		}
		return nil
	}
	s.remove = func(r *os.Root, n string) error { removed++; return r.Remove(n) }
	g, e := inventory(t.Context(), s, h)
	if e != nil {
		t.Fatal(e)
	}
	defer g.Close()
	if e = g.Retire(t.Context()); e == nil || !expired || removed != 0 {
		t.Fatal("unlink after clock expiry", e, expired, removed)
	}
	if _, e = os.Lstat(target); e != nil {
		t.Fatal("effect despite late expiry", e)
	}
}

func TestFinalRootLateAdmissionDriftPreservesRoot(t *testing.T) {
	for _, mode := range []string{"parent-mode", "parent-acl", "volume-mode", "parent-member"} {
		t.Run(mode, func(t *testing.T) {
			s, h := inventoryFixture(t)
			parentCalls, volumeCalls := 0, 0
			changed, rootACL := false, false
			mutate := func() {
				changed = true
				switch mode {
				case "parent-mode", "volume-mode":
					if e := os.Chmod(s.root, 0777); e != nil {
						t.Fatal(e)
					}
				case "parent-acl":
					rootACL = true
				case "parent-member":
					if e := os.WriteFile(filepath.Join(s.root, "late-unknown"), []byte("preserve"), 0600); e != nil {
						t.Fatal(e)
					}
				}
			}
			s.acl = callbackACL(func(p string) (bool, error) {
				if p == filepath.Dir(s.root) {
					parentCalls++
					if parentCalls == 5 && mode != "volume-mode" {
						mutate()
					}
				}
				return p == s.root && rootACL, nil
			})
			s.volume = func() error {
				volumeCalls++
				if volumeCalls == 3 && mode == "volume-mode" {
					mutate()
				}
				return nil
			}
			g, e := inventory(t.Context(), s, h)
			if e != nil {
				t.Fatal(e)
			}
			defer g.Close()
			e = g.Retire(t.Context())
			if e == nil || !changed {
				t.Fatal("late root drift accepted", mode, e, changed, parentCalls, volumeCalls)
			}
			if _, e = os.Lstat(s.root); e != nil {
				t.Fatal("root removed after unproved final security", e)
			}
		})
	}
}
