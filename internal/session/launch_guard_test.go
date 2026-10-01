package session

import (
	"context"
	"errors"
	"github.com/weshofmann/boxwarden/internal/supervisor"
	"testing"
	"time"
)

type startGuardFixture struct {
	held                       bool
	checked, released, checkAt int
	checkErr, releaseErr       error
}

func (g *startGuardFixture) Revalidate(context.Context) error {
	g.checked++
	if g.checkAt != 0 && g.checked < g.checkAt {
		return nil
	}
	return g.checkErr
}
func (g *startGuardFixture) Release() error { g.held = false; g.released++; return g.releaseErr }

// Removing the pre-intent acquisition/revalidation admits PrepareStart despite
// refusal. Releasing it before StartExact lets EX cross the detached handoff.
func TestLaunchGuardPreIntentAndHandoffLifetime(t *testing.T) {
	for _, refuse := range []bool{false, true} {
		t.Run(map[bool]string{false: "held", true: "refused"}[refuse], func(t *testing.T) {
			configured, observer, creator := createFixture(t)
			if _, err := creator.Create(t.Context(), "dev", ModeClean); err != nil {
				t.Fatal(err)
			}
			g := &startGuardFixture{held: true}
			if refuse {
				g.checkErr = errors.New("admission drift")
			}
			service := newStartTestService(configured, observer, &startSupervisorFake{start: func(r supervisor.LaunchRequest) (supervisor.Snapshot, error) {
				if !g.held || g.checked == 0 {
					t.Fatal("guard not held/revalidated at handoff")
				}
				return startedSnapshot(r.Binding, time.Now()), nil
			}}, time.Now, func() (string, error) { return testStartGeneration, nil })
			acquired := false
			prepared := false
			service.start.AcquireLaunchGuard = func(context.Context, RuntimeAdmission) (LaunchGuard, error) { acquired = true; return g, nil }
			service.start.Workspaces = startWorkspaceFake{prepare: func() error {
				prepared = true
				if !g.held || g.checked == 0 {
					t.Fatal("intent preceded guard")
				}
				return nil
			}}
			_, err := service.Start(t.Context(), "dev")
			if !acquired || g.released != 1 {
				t.Fatalf("acquired=%v released=%d", acquired, g.released)
			}
			if refuse {
				if !errors.Is(err, g.checkErr) || prepared {
					t.Fatalf("refusal=%v prepared=%v", err, prepared)
				}
				assertStoredState(t, configured, "dev", StateStopped)
			} else if err != nil || !prepared {
				t.Fatalf("start=%v prepared=%v", err, prepared)
			}
		})
	}
}

func TestLaunchGuardReleaseErrorRetainsOriginalFailure(t *testing.T) {
	configured, observer, creator := createFixture(t)
	if _, err := creator.Create(t.Context(), "dev", ModeClean); err != nil {
		t.Fatal(err)
	}
	cause := errors.New("prepare failed")
	closeErr := errors.New("close failed")
	g := &startGuardFixture{held: true, releaseErr: closeErr}
	service := newStartTestService(configured, observer, &startSupervisorFake{}, time.Now, func() (string, error) { return testStartGeneration, nil })
	service.start.AcquireLaunchGuard = func(context.Context, RuntimeAdmission) (LaunchGuard, error) { return g, nil }
	service.start.Workspaces = startWorkspaceFake{prepare: func() error { return cause }}
	_, err := service.Start(t.Context(), "dev")
	if !errors.Is(err, cause) || !errors.Is(err, closeErr) {
		t.Fatalf("lost failures: %v", err)
	}
}
func TestLaunchGuardRevalidatesDelayedBeforeIntent(t *testing.T) {
	configured, observer, creator := createFixture(t)
	creator.Create(t.Context(), "dev", ModeClean)
	g := &startGuardFixture{held: true, checkErr: errors.New("descriptor removed after original admission"), checkAt: 2}
	s := newStartTestService(configured, observer, &startSupervisorFake{}, time.Now, func() (string, error) { return testStartGeneration, nil })
	s.start.AcquireLaunchGuard = func(context.Context, RuntimeAdmission) (LaunchGuard, error) { return g, nil }
	prepared := false
	s.start.Workspaces = startWorkspaceFake{prepare: func() error { prepared = true; return nil }}
	if _, err := s.Start(t.Context(), "dev"); !errors.Is(err, g.checkErr) || prepared || g.released != 1 {
		t.Fatalf("delayed guard admitted intent: %v prepared=%v release=%d", err, prepared, g.released)
	}
	assertStoredState(t, configured, "dev", StateStopped)
}
func TestLaunchGuardPrecedesRebuildIntent(t *testing.T) {
	r, b, old := rebuildPreparationFixture(t)
	if _, err := r.PrepareCandidate(t.Context(), "dev", "golden-r2"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Cutover(t.Context(), "dev"); err != nil {
		t.Fatal(err)
	}
	j, err := LoadRebuildJournal(r.domain.StateRoot, old.Domain, "dev")
	if err != nil {
		t.Fatal(err)
	}
	g := &startGuardFixture{held: true, checkErr: errors.New("rebuild refused")}
	s := newStartTestService(r.domain, b, &startSupervisorFake{}, time.Now, func() (string, error) { return testStartGeneration, nil })
	s.start.AcquireLaunchGuard = func(context.Context, RuntimeAdmission) (LaunchGuard, error) { return g, nil }
	prepared := false
	s.start.Workspaces = startWorkspaceFake{rebuildPrepare: func(RebuildJournal) error { prepared = true; return nil }}
	if _, err := s.StartRebuildCandidate(t.Context(), "dev", j); !errors.Is(err, g.checkErr) || prepared || g.released != 1 {
		t.Fatalf("rebuild intent crossed refused guard: %v prepared=%v release=%d", err, prepared, g.released)
	}
}
