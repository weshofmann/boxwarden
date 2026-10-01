package closeout

import (
	"context"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/contract"
	"os"
	"path/filepath"
	"testing"
)

func configFixture(t *testing.T) (string, [5]string) {
	t.Helper()
	r, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	os.Chmod(r, 0700)
	var hashes [5]string
	for i, n := range configNames {
		b := []byte("{\"version\":1,\"id\":" + string(rune('1'+i)) + "}")
		if e = os.WriteFile(filepath.Join(r, n), b, 0600); e != nil {
			t.Fatal(e)
		}
		hashes[i] = contract.SHA(b)
	}
	return r, hashes
}

func TestConfigExpiryDuringFinalACLDoesNotDispatchUnlink(t *testing.T) {
	p, hashes := configFixture(t)
	expired, checks, removed := false, 0, 0
	target := filepath.Join(p, configNames[3])
	acl := callbackACL(func(n string) (bool, error) {
		if n == target {
			checks++
			if checks == 3 {
				expired = true
			}
		}
		return false, nil
	})
	tick := func() error {
		if expired {
			return ErrRefused
		}
		return nil
	}
	remove := func(r *os.Root, n string) error { removed++; return r.Remove(n) }
	e := retireEnrolled(t.Context(), p, os.Getuid(), acl, hashes, tick, remove)
	if e == nil || !expired || removed != 0 {
		t.Fatal("config unlink after clock expiry", e, expired, removed)
	}
	if _, e = os.Lstat(target); e != nil {
		t.Fatal("effect despite late expiry", e)
	}
}
func TestRetireOnlyExactEnrolledConfigs(t *testing.T) {
	for _, mode := range []string{"positive", "unknown", "hash", "link", "mode", "expired", "write-return"} {
		t.Run(mode, func(t *testing.T) {
			r, hashes := configFixture(t)
			tick := func() error { return nil }
			remove := func(root *os.Root, n string) error { return root.Remove(n) }
			switch mode {
			case "unknown":
				os.WriteFile(filepath.Join(r, "foreign.json"), []byte("{}"), 0600)
			case "hash":
				hashes[3] = contract.SHA([]byte("foreign"))
			case "link":
				os.Link(filepath.Join(r, configNames[3]), filepath.Join(r, "linked"))
			case "mode":
				os.Chmod(filepath.Join(r, configNames[3]), 0644)
			case "expired":
				tick = func() error { return context.DeadlineExceeded }
			case "write-return":
				remove = func(root *os.Root, n string) error {
					if e := root.Remove(n); e != nil {
						return e
					}
					return context.DeadlineExceeded
				}
			}
			e := retireEnrolled(t.Context(), r, os.Getuid(), noACL{}, hashes, tick, remove)
			if mode == "positive" {
				if e != nil {
					t.Fatal(e)
				}
				for _, n := range configNames[3:] {
					if _, e = os.Lstat(filepath.Join(r, n)); !os.IsNotExist(e) {
						t.Fatal("enrolled target retained", e)
					}
				}
			} else if e == nil {
				t.Fatal("uncertain config retirement succeeded")
			}
			for _, n := range configNames[:3] {
				if _, e = os.Lstat(filepath.Join(r, n)); e != nil {
					t.Fatal("source/doctor config removed", e)
				}
			}
			if mode == "unknown" || mode == "hash" || mode == "link" || mode == "mode" || mode == "expired" {
				for _, n := range configNames[3:] {
					if _, e = os.Lstat(filepath.Join(r, n)); e != nil {
						t.Fatal("effect before full admission", mode, e)
					}
				}
			}
		})
	}
}
