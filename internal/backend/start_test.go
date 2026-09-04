package backend

import (
	"context"
	"testing"
)

type starterFake struct {
	request StartRequest
	handle  Handle
}

func (s *starterFake) Start(_ context.Context, request StartRequest) (Handle, error) {
	s.request = request
	return s.handle, nil
}

type handleFake struct{ stopped, waited bool }

func (h *handleFake) Stop(context.Context) error { h.stopped = true; return nil }
func (h *handleFake) Wait(context.Context) error { h.waited = true; return nil }

func TestStarterExposesOnlyOwnedLaunchInputsAndHandle(t *testing.T) {
	handle := &handleFake{}
	starter := &starterFake{handle: handle}
	request := StartRequest{ObjectID: "boxwarden-work-dev", SerialDevice: "/dev/ttys004", GenerationDirectory: "/private/runtime/work/dev/generation-1"}

	got, err := starter.Start(context.Background(), request)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if starter.request != request || got != handle {
		t.Fatalf("Start() = (%#v, %#v), want request %#v and owned handle", starter.request, got, request)
	}
	if err := got.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if err := got.Wait(context.Background()); err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
	if !handle.stopped || !handle.waited {
		t.Fatalf("handle state = %#v, want explicit stop and wait", handle)
	}
}

func TestAddressResolverReturnsCurrentUnpersistedAddress(t *testing.T) {
	resolver := addressResolverFake{address: "192.0.2.10"}
	got, err := resolver.Resolve(context.Background(), "boxwarden-work-dev")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if got != "192.0.2.10" || resolver.objectID != "boxwarden-work-dev" {
		t.Fatalf("Resolve() = %q for %q, want current address and exact object", got, resolver.objectID)
	}
}

type addressResolverFake struct {
	address  string
	objectID string
}

func (r *addressResolverFake) Resolve(_ context.Context, objectID string) (string, error) {
	r.objectID = objectID
	return r.address, nil
}
