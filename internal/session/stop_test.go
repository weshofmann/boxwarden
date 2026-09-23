package session

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/weshofmann/boxwarden/internal/backend"
	"github.com/weshofmann/boxwarden/internal/backend/fake"
	"github.com/weshofmann/boxwarden/internal/supervisor"
)

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
