package fake_test

import (
	"context"
	"testing"

	"github.com/weshofmann/boxwarden/internal/backend"
	"github.com/weshofmann/boxwarden/internal/backend/fake"
)

func TestObserverReturnsConfiguredObservation(t *testing.T) {
	expected := backend.Observation{ObjectID: "boxwarden-work-dev", Exists: true, State: backend.ObjectStopped}
	observer := fake.Observer{Observations: map[string]backend.Observation{expected.ObjectID: expected}}

	got, err := observer.Observe(context.Background(), expected.ObjectID)
	if err != nil {
		t.Fatalf("Observe() error = %v", err)
	}
	if got != expected {
		t.Fatalf("Observe() = %#v, want %#v", got, expected)
	}
}

func TestObserverReportsAnAbsentUnconfiguredObject(t *testing.T) {
	observer := fake.Observer{}

	got, err := observer.Observe(context.Background(), "boxwarden-work-missing")
	if err != nil {
		t.Fatalf("Observe() error = %v", err)
	}
	if got.Exists {
		t.Fatalf("Observe().Exists = true, want false")
	}
	if got.State != backend.ObjectUnknown {
		t.Fatalf("Observe().State = %q, want %q", got.State, backend.ObjectUnknown)
	}
}
