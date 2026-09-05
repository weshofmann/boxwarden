package fake_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/weshofmann/boxwarden/internal/backend"
	"github.com/weshofmann/boxwarden/internal/backend/fake"
)

func TestBackendCloneCreatesTargetAndRecordsCall(t *testing.T) {
	backendFake := fake.New(backend.Observation{
		ObjectID: "golden-work",
		Exists:   true,
		State:    backend.ObjectStopped,
	})

	if err := backendFake.Clone(context.Background(), "golden-work", "boxwarden-work-dev"); err != nil {
		t.Fatalf("Clone() error = %v", err)
	}

	got, err := backendFake.Observe(context.Background(), "boxwarden-work-dev")
	if err != nil {
		t.Fatalf("Observe() error = %v", err)
	}
	want := backend.Observation{ObjectID: "boxwarden-work-dev", Exists: true, State: backend.ObjectStopped}
	if got != want {
		t.Fatalf("Observe() = %#v, want %#v", got, want)
	}
	if got, want := backendFake.CloneCalls(), []fake.CloneCall{{SourceID: "golden-work", TargetID: "boxwarden-work-dev"}}; !sameCloneCalls(got, want) {
		t.Fatalf("CloneCalls() = %#v, want %#v", got, want)
	}
}

func TestBackendCloneRejectsCollisionWithoutOverwritingTarget(t *testing.T) {
	backendFake := fake.New(
		backend.Observation{ObjectID: "golden-work", Exists: true, State: backend.ObjectStopped},
		backend.Observation{ObjectID: "boxwarden-work-dev", Exists: true, State: backend.ObjectRunning},
	)

	if err := backendFake.Clone(context.Background(), "golden-work", "boxwarden-work-dev"); err == nil {
		t.Fatal("Clone() error = nil, want collision error")
	}

	got, err := backendFake.Observe(context.Background(), "boxwarden-work-dev")
	if err != nil {
		t.Fatalf("Observe() error = %v", err)
	}
	want := backend.Observation{ObjectID: "boxwarden-work-dev", Exists: true, State: backend.ObjectRunning}
	if got != want {
		t.Fatalf("Observe() after collision = %#v, want %#v", got, want)
	}
	if got, want := backendFake.CloneCalls(), []fake.CloneCall{{SourceID: "golden-work", TargetID: "boxwarden-work-dev"}}; !sameCloneCalls(got, want) {
		t.Fatalf("CloneCalls() = %#v, want %#v", got, want)
	}
}

func TestBackendOperationFaultsLeaveObjectStateUnchanged(t *testing.T) {
	cloneFailure := errors.New("clone unavailable")
	macFailure := errors.New("mac mutation unavailable")
	backendFake := fake.New(backend.Observation{ObjectID: "golden-work", Exists: true, State: backend.ObjectStopped})
	backendFake.SetCloneError(cloneFailure)

	if err := backendFake.Clone(context.Background(), "golden-work", "boxwarden-work-dev"); !errors.Is(err, cloneFailure) {
		t.Fatalf("Clone() error = %v, want %v", err, cloneFailure)
	}
	backendFake.SetRandomizeMACError(macFailure)
	if err := backendFake.RandomizeMAC(context.Background(), "golden-work"); !errors.Is(err, macFailure) {
		t.Fatalf("RandomizeMAC() error = %v, want %v", err, macFailure)
	}

	got, err := backendFake.Observe(context.Background(), "boxwarden-work-dev")
	if err != nil {
		t.Fatalf("Observe() error = %v", err)
	}
	if got.Exists {
		t.Fatalf("Observe() after failed clone = %#v, want absent target", got)
	}
	if got, want := backendFake.RandomizeMACCalls(), []string{"golden-work"}; !sameStrings(got, want) {
		t.Fatalf("RandomizeMACCalls() = %#v, want %#v", got, want)
	}
}

func TestBackendFaultHooksLeaveObjectStateUnchanged(t *testing.T) {
	cloneFailure := errors.New("clone fault")
	macFailure := errors.New("MAC fault")
	backendFake := fake.New(backend.Observation{ObjectID: "golden-work", Exists: true, State: backend.ObjectStopped})
	backendFake.SetCloneFault(func(context.Context, fake.CloneCall) error { return cloneFailure })
	backendFake.SetRandomizeMACFault(func(context.Context, string) error { return macFailure })

	if err := backendFake.Clone(context.Background(), "golden-work", "boxwarden-work-dev"); !errors.Is(err, cloneFailure) {
		t.Fatalf("Clone() error = %v, want %v", err, cloneFailure)
	}
	if err := backendFake.RandomizeMAC(context.Background(), "golden-work"); !errors.Is(err, macFailure) {
		t.Fatalf("RandomizeMAC() error = %v, want %v", err, macFailure)
	}

	got, err := backendFake.Observe(context.Background(), "boxwarden-work-dev")
	if err != nil {
		t.Fatalf("Observe() error = %v", err)
	}
	if got.Exists {
		t.Fatalf("Observe() after failed clone = %#v, want absent target", got)
	}
}

func TestBackendCloneFaultCanReenterObservation(t *testing.T) {
	backendFake := fake.New(backend.Observation{ObjectID: "golden-work", Exists: true, State: backend.ObjectStopped})
	backendFake.SetCloneFault(func(ctx context.Context, _ fake.CloneCall) error {
		_, err := backendFake.Observe(ctx, "golden-work")
		return err
	})

	if err := backendFake.Clone(context.Background(), "golden-work", "boxwarden-work-dev"); err != nil {
		t.Fatalf("Clone() error = %v", err)
	}
}

func TestBackendClonePostEffectFailureLeavesTargetForRetryReconciliation(t *testing.T) {
	postEffectFailure := errors.New("clone response interrupted")
	backendFake := fake.New(backend.Observation{ObjectID: "golden-work", Exists: true, State: backend.ObjectStopped})
	backendFake.SetClonePostEffectError(postEffectFailure)

	if err := backendFake.Clone(context.Background(), "golden-work", "boxwarden-work-dev"); !errors.Is(err, postEffectFailure) {
		t.Fatalf("Clone() error = %v, want %v", err, postEffectFailure)
	}
	got, err := backendFake.Observe(context.Background(), "boxwarden-work-dev")
	if err != nil {
		t.Fatalf("Observe() error = %v", err)
	}
	want := backend.Observation{ObjectID: "boxwarden-work-dev", Exists: true, State: backend.ObjectStopped}
	if got != want {
		t.Fatalf("Observe() after post-effect failure = %#v, want %#v", got, want)
	}
	if err := backendFake.Clone(context.Background(), "golden-work", "boxwarden-work-dev"); err == nil || errors.Is(err, postEffectFailure) {
		t.Fatalf("retry Clone() error = %v, want collision rather than post-effect failure", err)
	}
}

func TestBackendRandomizeMACPostEffectFailureRecordsCall(t *testing.T) {
	postEffectFailure := errors.New("MAC response interrupted")
	backendFake := fake.New(backend.Observation{ObjectID: "boxwarden-work-dev", Exists: true, State: backend.ObjectStopped})
	backendFake.SetRandomizeMACPostEffectError(postEffectFailure)

	if err := backendFake.RandomizeMAC(context.Background(), "boxwarden-work-dev"); !errors.Is(err, postEffectFailure) {
		t.Fatalf("RandomizeMAC() error = %v, want %v", err, postEffectFailure)
	}
	if got, want := backendFake.RandomizeMACCalls(), []string{"boxwarden-work-dev"}; !sameStrings(got, want) {
		t.Fatalf("RandomizeMACCalls() = %#v, want %#v", got, want)
	}
}

func TestBackendCanceledContextReturnsBeforeObservationOrMutation(t *testing.T) {
	backendFake := fake.New(backend.Observation{ObjectID: "golden-work", Exists: true, State: backend.ObjectStopped})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if got, err := backendFake.Observe(ctx, "golden-work"); !errors.Is(err, context.Canceled) || got != (backend.Observation{}) {
		t.Fatalf("Observe() = (%#v, %v), want empty observation and context cancellation", got, err)
	}
	if err := backendFake.Clone(ctx, "golden-work", "boxwarden-work-dev"); !errors.Is(err, context.Canceled) {
		t.Fatalf("Clone() error = %v, want context cancellation", err)
	}
	if err := backendFake.RandomizeMAC(ctx, "golden-work"); !errors.Is(err, context.Canceled) {
		t.Fatalf("RandomizeMAC() error = %v, want context cancellation", err)
	}
	if got := backendFake.CloneCalls(); len(got) != 0 {
		t.Fatalf("CloneCalls() = %#v, want no mutation", got)
	}
	if got := backendFake.RandomizeMACCalls(); len(got) != 0 {
		t.Fatalf("RandomizeMACCalls() = %#v, want no mutation", got)
	}
	got, err := backendFake.Observe(context.Background(), "boxwarden-work-dev")
	if err != nil {
		t.Fatalf("Observe() error = %v", err)
	}
	if got.Exists {
		t.Fatalf("Observe() after canceled Clone() = %#v, want absent target", got)
	}
}

func TestBackendRandomizeMACRecordsSuccessfulCall(t *testing.T) {
	backendFake := fake.New(backend.Observation{ObjectID: "boxwarden-work-dev", Exists: true, State: backend.ObjectStopped})

	if err := backendFake.RandomizeMAC(context.Background(), "boxwarden-work-dev"); err != nil {
		t.Fatalf("RandomizeMAC() error = %v", err)
	}
	if got, want := backendFake.RandomizeMACCalls(), []string{"boxwarden-work-dev"}; !sameStrings(got, want) {
		t.Fatalf("RandomizeMACCalls() = %#v, want %#v", got, want)
	}
}

func TestBackendConcurrentCloneCollisionPreservesOneTarget(t *testing.T) {
	backendFake := fake.New(backend.Observation{ObjectID: "golden-work", Exists: true, State: backend.ObjectStopped})
	const attempts = 32

	errorsByAttempt := make(chan error, attempts)
	var group sync.WaitGroup
	for attempt := 0; attempt < attempts; attempt++ {
		group.Add(1)
		go func() {
			defer group.Done()
			errorsByAttempt <- backendFake.Clone(context.Background(), "golden-work", "boxwarden-work-dev")
		}()
	}
	group.Wait()
	close(errorsByAttempt)

	successes := 0
	for err := range errorsByAttempt {
		if err == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("successful Clone() calls = %d, want 1", successes)
	}
	if got := len(backendFake.CloneCalls()); got != attempts {
		t.Fatalf("recorded Clone() calls = %d, want %d", got, attempts)
	}
	got, err := backendFake.Observe(context.Background(), "boxwarden-work-dev")
	if err != nil {
		t.Fatalf("Observe() error = %v", err)
	}
	want := backend.Observation{ObjectID: "boxwarden-work-dev", Exists: true, State: backend.ObjectStopped}
	if got != want {
		t.Fatalf("Observe() = %#v, want %#v", got, want)
	}
}

func TestBackendSetObservationReplacesObservedState(t *testing.T) {
	backendFake := fake.New(backend.Observation{ObjectID: "boxwarden-work-dev", Exists: true, State: backend.ObjectStopped})
	backendFake.SetObservation(backend.Observation{ObjectID: "boxwarden-work-dev", Exists: true, State: backend.ObjectRunning})

	got, err := backendFake.Observe(context.Background(), "boxwarden-work-dev")
	if err != nil {
		t.Fatalf("Observe() error = %v", err)
	}
	want := backend.Observation{ObjectID: "boxwarden-work-dev", Exists: true, State: backend.ObjectRunning}
	if got != want {
		t.Fatalf("Observe() = %#v, want %#v", got, want)
	}
}

func TestBackendDeleteObservationMakesObjectAbsent(t *testing.T) {
	backendFake := fake.New(backend.Observation{ObjectID: "boxwarden-work-dev", Exists: true, State: backend.ObjectStopped})
	backendFake.DeleteObservation("boxwarden-work-dev")

	got, err := backendFake.Observe(context.Background(), "boxwarden-work-dev")
	if err != nil {
		t.Fatalf("Observe() error = %v", err)
	}
	want := backend.Observation{ObjectID: "boxwarden-work-dev", State: backend.ObjectUnknown}
	if got != want {
		t.Fatalf("Observe() = %#v, want %#v", got, want)
	}
}

func TestBackendStartResolveAndDeleteRecordExactObjectOperations(t *testing.T) {
	backendFake := fake.New(backend.Observation{ObjectID: "boxwarden-work-dev", Exists: true, State: backend.ObjectStopped})
	request := backend.StartRequest{ObjectID: "boxwarden-work-dev", SerialDevice: "/dev/ttys004", GenerationDirectory: "/private/runtime/work/dev/generation-1"}

	handle, err := backendFake.Start(context.Background(), request)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if got, want := backendFake.StartCalls(), []fake.StartCall{{Request: request}}; !sameStartCalls(got, want) {
		t.Fatalf("StartCalls() = %#v, want %#v", got, want)
	}
	backendFake.SetAddress(request.ObjectID, "192.0.2.10")
	if got, err := backendFake.Resolve(context.Background(), request.ObjectID); err != nil || got != "192.0.2.10" {
		t.Fatalf("Resolve() = (%q, %v), want configured current address", got, err)
	}
	if err := handle.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if err := handle.Wait(context.Background()); err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
	if err := backendFake.Delete(context.Background(), request.ObjectID); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if got, want := backendFake.DeleteCalls(), []string{request.ObjectID}; !sameStrings(got, want) {
		t.Fatalf("DeleteCalls() = %#v, want %#v", got, want)
	}
	if got, err := backendFake.Observe(context.Background(), request.ObjectID); err != nil || got.Exists {
		t.Fatalf("Observe() after Delete() = (%#v, %v), want absent", got, err)
	}
}

func sameStartCalls(got, want []fake.StartCall) bool {
	if len(got) != len(want) {
		return false
	}
	for index := range got {
		if got[index] != want[index] {
			return false
		}
	}
	return true
}

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

func sameCloneCalls(got, want []fake.CloneCall) bool {
	if len(got) != len(want) {
		return false
	}
	for index := range got {
		if got[index] != want[index] {
			return false
		}
	}
	return true
}

func sameStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for index := range got {
		if got[index] != want[index] {
			return false
		}
	}
	return true
}
