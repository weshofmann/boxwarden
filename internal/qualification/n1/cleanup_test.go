package n1

import (
	"context"
	"errors"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/contract"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/fixed"
	"testing"
)

type engineGuard struct {
	fail            string
	removed, closed bool
}

func (g *engineGuard) Revalidate(context.Context) error {
	if g.fail == "revalidate" {
		return ErrRefused
	}
	return nil
}
func (g *engineGuard) RemoveExact(context.Context) error {
	g.removed = true
	if g.fail == "remove" {
		return ErrRefused
	}
	return nil
}
func (g *engineGuard) Close() error {
	g.closed = true
	if g.fail == "close" {
		return ErrRefused
	}
	return nil
}
func TestCleanupReturnAndCheckedClosuresPrecedePublication(t *testing.T) {
	for _, fail := range []string{"", "acquire", "inventory", "storage", "census", "check", "revalidate", "remove", "close", "state-close", "publish"} {
		t.Run(fail, func(t *testing.T) {
			g := &engineGuard{fail: fail}
			state := &engineGuard{}
			if fail == "state-close" {
				state.fail = "close"
			}
			published := false
			d := cleanupDependencies{acquire: func(context.Context) (cleanupGuard, error) {
				if fail == "acquire" {
					return nil, ErrRefused
				}
				return g, nil
			}, inventory: func(context.Context) (stateGuard, error) {
				if fail == "inventory" {
					return nil, ErrRefused
				}
				return state, nil
			}, publish: func(c contract.Completion) error {
				if !g.removed || !g.closed || !state.closed || !c.HandlesClosed {
					t.Fatal("publication preceded acknowledged operation/closure")
				}
				published = true
				if fail == "publish" {
					return ErrRefused
				}
				return nil
			}}
			for name, p := range map[string]*func(context.Context) error{"storage": &d.storage, "census": &d.census, "check": &d.check} {
				n := name
				*p = func(context.Context) error {
					if fail == n {
						return errors.New("injected admission failure")
					}
					return nil
				}
			}
			e := executeCleanup(t.Context(), fixed.Inputs{}, d)
			if fail == "" && e != nil {
				t.Fatal(e)
			}
			if fail != "" && e == nil {
				t.Fatal("unknown returned success")
			}
			if fail != "" && fail != "publish" && published {
				t.Fatal("failed outcome certified")
			}
		})
	}
}
