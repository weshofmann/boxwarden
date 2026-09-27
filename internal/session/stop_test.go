package session

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/weshofmann/boxwarden/internal/backend"
	"github.com/weshofmann/boxwarden/internal/backend/fake"
	"github.com/weshofmann/boxwarden/internal/lock"
	"github.com/weshofmann/boxwarden/internal/supervisor"
)

func TestStopReleasesSessionLockDuringSupervisorStop(t *testing.T) {
	domainConfig, backendFake, creator := createFixture(t)
	created, err := creator.Create(context.Background(), "dev", ModeClean)
	if err != nil {
		t.Fatal(err)
	}
	running := saveRunningRecord(t, domainConfig, created)
	backendFake.SetObservation(backend.Observation{ObjectID: running.Backend.ObjectID, Exists: true, State: backend.ObjectRunning})
	control := &startSupervisorFake{stop: func(supervisor.Binding) error {
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer cancel()
		held, err := lock.AcquireSession(ctx, domainConfig.StateRoot, "work", "dev")
		if err != nil {
			return err
		}
		if err := held.Release(); err != nil {
			return err
		}
		backendFake.SetObservation(backend.Observation{ObjectID: running.Backend.ObjectID, Exists: true, State: backend.ObjectStopped})
		return nil
	}}
	service := newStartTestService(domainConfig, backendFake, control, time.Now, nil)
	if _, err := service.Stop(context.Background(), "dev"); err != nil {
		t.Fatalf("supervisor stop was blocked by parent session lock: %v", err)
	}
}

func TestStopPersistsIntentBeforeExactSupervisorStop(t *testing.T) {
	domainConfig, backendFake, creator := createFixture(t)
	created, err := creator.Create(context.Background(), "dev", ModeClean)
	if err != nil {
		t.Fatal(err)
	}
	running := saveRunningRecord(t, domainConfig, created)
	backendFake.SetObservation(backend.Observation{ObjectID: running.Backend.ObjectID, Exists: true, State: backend.ObjectRunning})
	control := &startSupervisorFake{stop: func(binding supervisor.Binding) error {
		stored, err := LoadRecord(domainConfig.StateRoot, "work", "dev")
		if err != nil {
			return err
		}
		if stored.IntendedState != StateStopping || stored.StartGeneration != running.StartGeneration || stored.Readiness.Status != ReadinessNotReady {
			t.Fatalf("record at stop mutation = %#v", stored)
		}
		if binding != startBinding(running) {
			t.Fatalf("stop binding = %#v", binding)
		}
		backendFake.SetObservation(backend.Observation{ObjectID: running.Backend.ObjectID, Exists: true, State: backend.ObjectStopped})
		return nil
	}}
	service := newStartTestService(domainConfig, backendFake, control, time.Now, nil)
	stopped, err := service.Stop(context.Background(), "dev")
	if err != nil {
		t.Fatal(err)
	}
	if stopped.IntendedState != StateStopped || stopped.StartGeneration != "" || stopped.Readiness.Status != ReadinessNotReady || control.stopCalls != 1 {
		t.Fatalf("stopped record = %#v; calls=%d", stopped, control.stopCalls)
	}
	assertStoredRecord(t, domainConfig, stopped)
	if again, err := service.Stop(context.Background(), "dev"); err != nil || again != stopped || control.stopCalls != 1 {
		t.Fatalf("idempotent stop = %#v, %v; calls=%d", again, err, control.stopCalls)
	}
}

type outcomeSupervisorFake struct {
	*startSupervisorFake
	outcome supervisor.StopOutcome
}

func (f *outcomeSupervisorFake) StopWithOutcome(ctx context.Context, binding supervisor.Binding) (supervisor.StopOutcome, error) {
	if err := f.Stop(ctx, binding); err != nil {
		return supervisor.StopOutcome{}, err
	}
	return f.outcome, nil
}

func TestStopPersistsObservedOutcomeWithoutClaimingCleanVolume(t *testing.T) {
	domainConfig, backendFake, creator := createFixture(t)
	created, err := creator.Create(context.Background(), "dev", ModeClean)
	if err != nil {
		t.Fatal(err)
	}
	running := saveRunningRecord(t, domainConfig, created)
	backendFake.SetObservation(backend.Observation{ObjectID: running.Backend.ObjectID, Exists: true, State: backend.ObjectRunning})
	control := &outcomeSupervisorFake{
		startSupervisorFake: &startSupervisorFake{stop: func(supervisor.Binding) error {
			backendFake.SetObservation(backend.Observation{ObjectID: running.Backend.ObjectID, Exists: true, State: backend.ObjectStopped})
			return nil
		}},
		outcome: supervisor.StopOutcome{Request: supervisor.StopRequestTartFallback, Forced: true},
	}
	service := newStartTestService(domainConfig, backendFake, control, time.Now, nil)
	stopped, err := service.Stop(context.Background(), "dev")
	if err != nil {
		t.Fatal(err)
	}
	want := "request=tart_fallback forced=true workspace_cleanliness=unverified"
	if stopped.Readiness.Diagnostic != want {
		t.Fatalf("stop diagnostic = %q, want %q", stopped.Readiness.Diagnostic, want)
	}
	assertStoredRecord(t, domainConfig, stopped)
}

func TestStopFailureRetainsExactStoppingIntent(t *testing.T) {
	domainConfig, backendFake, creator := createFixture(t)
	created, err := creator.Create(context.Background(), "dev", ModeClean)
	if err != nil {
		t.Fatal(err)
	}
	running := saveRunningRecord(t, domainConfig, created)
	backendFake.SetObservation(backend.Observation{ObjectID: running.Backend.ObjectID, Exists: true, State: backend.ObjectRunning})
	stopError := errors.New("exact owner unavailable")
	control := &startSupervisorFake{stop: func(supervisor.Binding) error { return stopError }}
	service := newStartTestService(domainConfig, backendFake, control, time.Now, nil)
	if _, err := service.Stop(context.Background(), "dev"); !errors.Is(err, stopError) {
		t.Fatalf("Stop error = %v", err)
	}
	stored := assertStoredState(t, domainConfig, "dev", StateStopping)
	if stored.StartGeneration != running.StartGeneration || stored.Readiness.Status != ReadinessNotReady {
		t.Fatalf("failed stop lost exact generation = %#v", stored)
	}
	if _, err := service.Stop(context.Background(), "dev"); !errors.Is(err, stopError) || control.stopCalls != 2 {
		t.Fatalf("retry error = %v; calls=%d", err, control.stopCalls)
	}
}

func TestStopReleasesUsesBeforePersistingStopped(t *testing.T) {
	domainConfig, backendFake, creator := createFixture(t)
	created, err := creator.Create(context.Background(), "dev", ModeClean)
	if err != nil {
		t.Fatal(err)
	}
	running := saveRunningRecord(t, domainConfig, created)
	backendFake.SetObservation(backend.Observation{ObjectID: running.Backend.ObjectID, Exists: true, State: backend.ObjectRunning})
	control := &startSupervisorFake{stop: func(supervisor.Binding) error {
		backendFake.SetObservation(backend.Observation{ObjectID: running.Backend.ObjectID, Exists: true, State: backend.ObjectStopped})
		return nil
	}}
	service := newStartTestService(domainConfig, backendFake, control, time.Now, nil)
	injected := errors.New("workspace release failed")
	releaseCalls := 0
	service.start.Workspaces = startWorkspaceFake{release: func() error {
		releaseCalls++
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer cancel()
		held, err := lock.AcquireSession(ctx, domainConfig.StateRoot, "work", "dev")
		if err != nil {
			return err
		}
		defer held.Release()
		current, err := LoadRecord(domainConfig.StateRoot, "work", "dev")
		if err != nil || current.IntendedState != StateStopping {
			return errors.New("workspace release ran after Stopped marker")
		}
		if releaseCalls == 1 {
			return injected
		}
		return nil
	}}
	if _, err := service.Stop(context.Background(), "dev"); !errors.Is(err, injected) {
		t.Fatalf("Stop() error = %v, want release failure", err)
	}
	assertStoredState(t, domainConfig, "dev", StateStopping)
	stopped, err := service.Stop(context.Background(), "dev")
	if err != nil || stopped.IntendedState != StateStopped || releaseCalls != 2 {
		t.Fatalf("release retry = %#v, %v, calls=%d", stopped, err, releaseCalls)
	}
}

func TestStopRetryReconcilesReapedSocketGoneGeneration(t *testing.T) {
	domainConfig, backendFake, creator := createFixture(t)
	created, err := creator.Create(context.Background(), "dev", ModeClean)
	if err != nil {
		t.Fatal(err)
	}
	running := saveRunningRecord(t, domainConfig, created)
	backendFake.SetObservation(backend.Observation{ObjectID: running.Backend.ObjectID, Exists: true, State: backend.ObjectRunning})
	stopError := errors.New("control reply timed out")
	quiesced := false
	control := &startSupervisorFake{
		stop: func(supervisor.Binding) error { return stopError },
		quiesced: func(binding supervisor.Binding) (bool, error) {
			if binding != startBinding(running) {
				t.Fatalf("quiescence binding = %#v", binding)
			}
			return quiesced, nil
		},
	}
	service := newStartTestService(domainConfig, backendFake, control, time.Now, nil)
	if _, err := service.Stop(context.Background(), "dev"); !errors.Is(err, stopError) {
		t.Fatalf("first Stop error = %v", err)
	}
	backendFake.SetObservation(backend.Observation{ObjectID: running.Backend.ObjectID, Exists: true, State: backend.ObjectStopped})
	if _, err := service.Stop(context.Background(), "dev"); !errors.Is(err, stopError) {
		t.Fatalf("unproven retry error = %v", err)
	}
	assertStoredState(t, domainConfig, "dev", StateStopping)
	quiesced = true
	stopped, err := service.Stop(context.Background(), "dev")
	if err != nil || stopped.IntendedState != StateStopped || stopped.StartGeneration != "" || control.stopCalls != 2 {
		t.Fatalf("quiesced retry = %#v, %v; stop calls=%d", stopped, err, control.stopCalls)
	}
}

func TestStopRejectsDriftBeforeIntentMutation(t *testing.T) {
	domainConfig, _, creator := createFixture(t)
	created, err := creator.Create(context.Background(), "dev", ModeClean)
	if err != nil {
		t.Fatal(err)
	}
	running := saveRunningRecord(t, domainConfig, created)
	observer := fake.Observer{Observations: map[string]backend.Observation{running.Backend.ObjectID: {ObjectID: "foreign-object", Exists: true, State: backend.ObjectRunning}}}
	control := &startSupervisorFake{}
	service := newStartTestService(domainConfig, observer, control, time.Now, nil)
	if _, err := service.Stop(context.Background(), "dev"); err == nil {
		t.Fatal("mismatched backend observation accepted")
	}
	assertStoredRecord(t, domainConfig, running)
	if control.stopCalls != 0 {
		t.Fatal("supervisor mutated despite backend drift")
	}
}

// A failed start can reap and remove its exact generation before Stop is called.
// The first Stop must persist stopping intent and accept the same quiescence
// proof as a retry, without contacting a control socket that no longer exists.
func TestStopReconcilesQuiescentGenerationOnFirstCall(t *testing.T) {
	for _, state := range []IntendedState{StateStarting, StateRunning} {
		t.Run(string(state), func(t *testing.T) {
			domainConfig, backendFake, creator := createFixture(t)
			created, err := creator.Create(context.Background(), "dev", ModeClean)
			if err != nil {
				t.Fatal(err)
			}
			prior := saveRunningRecord(t, domainConfig, created)
			prior.IntendedState = state
			if state == StateStarting {
				prior.Readiness = ReadinessRecord{Status: ReadinessStarting}
			}
			if err := SaveRecord(domainConfig.StateRoot, domainConfig.ID, prior); err != nil {
				t.Fatal(err)
			}
			backendFake.SetObservation(backend.Observation{ObjectID: prior.Backend.ObjectID, Exists: true, State: backend.ObjectStopped})
			proofCalls, releaseCalls := 0, 0
			control := &startSupervisorFake{
				stop: func(supervisor.Binding) error { return errors.New("cleaned generation has no control socket") },
				quiesced: func(binding supervisor.Binding) (bool, error) {
					proofCalls++
					if binding != startBinding(prior) {
						t.Fatalf("quiescence binding = %#v", binding)
					}
					stored := assertStoredState(t, domainConfig, "dev", StateStopping)
					if stored.StartGeneration != prior.StartGeneration || stored.Readiness.Status != ReadinessNotReady {
						t.Fatalf("intent at proof = %#v", stored)
					}
					return true, nil
				},
			}
			service := newStartTestService(domainConfig, backendFake, control, time.Now, nil)
			service.start.Workspaces = startWorkspaceFake{release: func() error {
				releaseCalls++
				ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
				defer cancel()
				held, err := lock.AcquireSession(ctx, domainConfig.StateRoot, "work", "dev")
				if err != nil {
					return err
				}
				defer held.Release()
				assertStoredState(t, domainConfig, "dev", StateStopping)
				return nil
			}}
			stopped, err := service.Stop(context.Background(), "dev")
			if err != nil {
				t.Fatalf("first Stop failed: %v", err)
			}
			if stopped.IntendedState != StateStopped || stopped.StartGeneration != "" || stopped.Readiness.Status != ReadinessNotReady || proofCalls != 1 || releaseCalls != 1 || control.stopCalls != 0 {
				t.Fatalf("first stop = %#v; proof=%d release=%d control=%d", stopped, proofCalls, releaseCalls, control.stopCalls)
			}
			assertStoredRecord(t, domainConfig, stopped)
		})
	}
}

func TestStopQuiescenceFailureRetainsGenerationOnFirstCall(t *testing.T) {
	for _, state := range []IntendedState{StateStarting, StateRunning} {
		for _, proofFails := range []bool{false, true} {
			name := string(state) + "/false"
			if proofFails {
				name = string(state) + "/error"
			}
			t.Run(name, func(t *testing.T) {
				domainConfig, backendFake, creator := createFixture(t)
				created, err := creator.Create(context.Background(), "dev", ModeClean)
				if err != nil {
					t.Fatal(err)
				}
				prior := saveRunningRecord(t, domainConfig, created)
				prior.IntendedState = state
				if state == StateStarting {
					prior.Readiness = ReadinessRecord{Status: ReadinessStarting}
				}
				if err := SaveRecord(domainConfig.StateRoot, domainConfig.ID, prior); err != nil {
					t.Fatal(err)
				}
				backendFake.SetObservation(backend.Observation{ObjectID: prior.Backend.ObjectID, Exists: true, State: backend.ObjectStopped})
				proofError, stopError := errors.New("unsafe exact runtime parent"), errors.New("exact owner unavailable")
				proofCalls, releaseCalls := 0, 0
				control := &startSupervisorFake{
					stop: func(supervisor.Binding) error { return stopError },
					quiesced: func(binding supervisor.Binding) (bool, error) {
						proofCalls++
						if binding != startBinding(prior) {
							t.Fatalf("quiescence binding = %#v", binding)
						}
						assertStoredState(t, domainConfig, "dev", StateStopping)
						if proofFails {
							return false, proofError
						}
						return false, nil
					},
				}
				service := newStartTestService(domainConfig, backendFake, control, time.Now, nil)
				service.start.Workspaces = startWorkspaceFake{release: func() error { releaseCalls++; return nil }}
				_, err = service.Stop(context.Background(), "dev")
				wantErr, wantStopCalls := stopError, 1
				if proofFails {
					wantErr, wantStopCalls = proofError, 0
				}
				if !errors.Is(err, wantErr) || proofCalls != 1 || control.stopCalls != wantStopCalls || releaseCalls != 0 {
					t.Fatalf("stop error=%v; proof=%d control=%d release=%d", err, proofCalls, control.stopCalls, releaseCalls)
				}
				stored := assertStoredState(t, domainConfig, "dev", StateStopping)
				if stored.StartGeneration != prior.StartGeneration || stored.Readiness.Status != ReadinessNotReady {
					t.Fatalf("failed stop lost generation: %#v", stored)
				}
			})
		}
	}
}
