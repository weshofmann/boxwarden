package session

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/weshofmann/boxwarden/internal/backend"
	"github.com/weshofmann/boxwarden/internal/config"
	"github.com/weshofmann/boxwarden/internal/domain"
	"github.com/weshofmann/boxwarden/internal/hostx"
	"github.com/weshofmann/boxwarden/internal/lock"
	"github.com/weshofmann/boxwarden/internal/sshx"
	"github.com/weshofmann/boxwarden/internal/supervisor"
)

const testStartGeneration = "11111111-2222-4333-8444-555555555555"

func TestStartRejectsMissingBoundRecipeIntentBeforeLaunch(t *testing.T) {
	configured, backendFake, creator := createFixture(t)
	record, err := creator.Create(t.Context(), "dev", ModeClean)
	if err != nil {
		t.Fatal(err)
	}
	record.RecipeIntentDigest = strings.Repeat("a", 64)
	if err := SaveRecord(configured.StateRoot, record.Domain, record); err != nil {
		t.Fatal(err)
	}
	starter := newStartTestService(configured, backendFake, &startSupervisorFake{start: func(supervisor.LaunchRequest) (supervisor.Snapshot, error) {
		t.Fatal("missing recipe intent reached supervisor")
		return supervisor.Snapshot{}, nil
	}}, time.Now, func() (string, error) { return testStartGeneration, nil })
	if _, err := starter.Start(t.Context(), "dev"); err == nil {
		t.Fatal("missing bound recipe intent started session")
	}
	assertStoredState(t, configured, "dev", StateStopped)
}

func TestStartDoesNotLaunchAfterWorkspaceReservationFailure(t *testing.T) {
	domainConfig, backendFake, creator := createFixture(t)
	if _, err := creator.Create(context.Background(), "dev", ModeClean); err != nil {
		t.Fatal(err)
	}
	service := newStartTestService(domainConfig, backendFake, &startSupervisorFake{start: func(supervisor.LaunchRequest) (supervisor.Snapshot, error) {
		t.Fatal("supervisor reached after workspace reservation failure")
		return supervisor.Snapshot{}, nil
	}}, time.Now, func() (string, error) { return testStartGeneration, nil })
	called := false
	service.start.Workspaces = startWorkspaceFake{prepare: func() error {
		called = true
		return errors.New("reservation failed before launch commit")
	}}
	if _, err := service.Start(context.Background(), "dev"); err == nil || !called {
		t.Fatalf("Start() error = %v, reservation called = %v", err, called)
	}
	assertStoredState(t, domainConfig, "dev", StateStopped)
}

func TestStartReconcilesStoppedUsesBeforeNewGeneration(t *testing.T) {
	domainConfig, backendFake, creator := createFixture(t)
	if _, err := creator.Create(context.Background(), "dev", ModeClean); err != nil {
		t.Fatal(err)
	}
	control := &startSupervisorFake{start: func(supervisor.LaunchRequest) (supervisor.Snapshot, error) {
		t.Fatal("supervisor reached before stopped-use reconciliation")
		return supervisor.Snapshot{}, nil
	}}
	service := newStartTestService(domainConfig, backendFake, control, time.Now, func() (string, error) {
		t.Fatal("generation allocated before stopped-use reconciliation")
		return "", nil
	})
	injected := errors.New("partial Use cannot yet be cleared")
	service.start.Workspaces = startWorkspaceFake{release: func() error { return injected }}
	if _, err := service.Start(context.Background(), "dev"); !errors.Is(err, injected) {
		t.Fatalf("Start() error = %v, want stopped reconciliation error", err)
	}
	assertStoredState(t, domainConfig, "dev", StateStopped)
}

func TestStartUpgradesLegacyStoppedRecordBeforeWorkspaceReservation(t *testing.T) {
	domainConfig, backendFake, creator := createFixture(t)
	created, err := creator.Create(context.Background(), "dev", ModeClean)
	if err != nil {
		t.Fatal(err)
	}
	writeRecord(t, domainConfig.StateRoot, "dev", fmt.Sprintf(`{"version":1,"domain":"work","name":"dev","id":%q,"mode":"clean","intended_state":"stopped","backend":{"kind":"tart","object_id":%q},"golden_revision":%q}`, created.ID, created.Backend.ObjectID, created.GoldenRevision))
	service := newStartTestService(domainConfig, backendFake, &startSupervisorFake{start: func(request supervisor.LaunchRequest) (supervisor.Snapshot, error) {
		return startedSnapshot(request.Binding, time.Now()), nil
	}}, time.Now, func() (string, error) { return testStartGeneration, nil })
	service.start.Workspaces = startWorkspaceFake{prepare: func() error {
		current, err := LoadRecord(domainConfig.StateRoot, "work", "dev")
		if err != nil || current.Version != recordVersion || current.IntendedState != StateStopped {
			return fmt.Errorf("legacy session not upgraded before reservation: %#v, %w", current, err)
		}
		return nil
	}}
	got, err := service.Start(context.Background(), "dev")
	if err != nil || got.Version != recordVersion || got.IntendedState != StateStarting {
		t.Fatalf("legacy start = %#v, %v", got, err)
	}
}

func TestStartReleasesSessionLockDuringSupervisorLaunch(t *testing.T) {
	domainConfig, backendFake, creator := createFixture(t)
	if _, err := creator.Create(context.Background(), "dev", ModeClean); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	control := &startSupervisorFake{start: func(request supervisor.LaunchRequest) (supervisor.Snapshot, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer cancel()
		held, err := lock.AcquireSession(ctx, domainConfig.StateRoot, "work", "dev")
		if err != nil {
			return supervisor.Snapshot{}, fmt.Errorf("supervisor could not acquire session lock: %w", err)
		}
		if err := held.Release(); err != nil {
			return supervisor.Snapshot{}, err
		}
		return startedSnapshot(request.Binding, now), nil
	}}
	service := newStartTestService(domainConfig, backendFake, control, func() time.Time { return now }, func() (string, error) { return testStartGeneration, nil })
	if _, err := service.Start(context.Background(), "dev"); err != nil {
		t.Fatal(err)
	}
}

func TestStopCannotPreemptInFlightExactStart(t *testing.T) {
	domainConfig, backendFake, creator := createFixture(t)
	if _, err := creator.Create(context.Background(), "dev", ModeClean); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	var service *Service
	control := &startSupervisorFake{
		start: func(request supervisor.LaunchRequest) (supervisor.Snapshot, error) {
			ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
			defer cancel()
			if _, err := service.Stop(ctx, "dev"); !errors.Is(err, context.DeadlineExceeded) {
				return supervisor.Snapshot{}, fmt.Errorf("stop preempted in-flight exact start: %v", err)
			}
			return readySnapshot(request.Binding, now), nil
		},
		stop: func(supervisor.Binding) error { return nil },
	}
	service = newStartTestService(domainConfig, backendFake, control, func() time.Time { return now }, func() (string, error) { return testStartGeneration, nil })
	if _, err := service.Start(context.Background(), "dev"); err != nil {
		t.Fatal(err)
	}
	if got := assertStoredState(t, domainConfig, "dev", StateRunning); got.StartGeneration != testStartGeneration || got.Readiness.Status != ReadinessReady {
		t.Fatalf("started record = %#v", got)
	}
}

func TestStartRejectsChangedIntentAfterSupervisorReturns(t *testing.T) {
	domainConfig, backendFake, creator := createFixture(t)
	if _, err := creator.Create(context.Background(), "dev", ModeClean); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	control := &startSupervisorFake{start: func(request supervisor.LaunchRequest) (supervisor.Snapshot, error) {
		held, err := lock.AcquireSession(context.Background(), domainConfig.StateRoot, "work", "dev")
		if err != nil {
			return supervisor.Snapshot{}, err
		}
		defer held.Release()
		current, err := LoadRecord(domainConfig.StateRoot, "work", "dev")
		if err != nil {
			return supervisor.Snapshot{}, err
		}
		current.IntendedState = StateStopping
		current.Readiness = ReadinessRecord{Status: ReadinessNotReady}
		if err := SaveRecord(domainConfig.StateRoot, domainConfig.ID, current); err != nil {
			return supervisor.Snapshot{}, err
		}
		return readySnapshot(request.Binding, now), nil
	}}
	service := newStartTestService(domainConfig, backendFake, control, func() time.Time { return now }, func() (string, error) { return testStartGeneration, nil })
	if _, err := service.Start(context.Background(), "dev"); err == nil {
		t.Fatal("changed intent was restored to READY")
	}
	if got := assertStoredState(t, domainConfig, "dev", StateStopping); got.Readiness.Status != ReadinessNotReady {
		t.Fatalf("changed intent was overwritten: %#v", got)
	}
}

// Production break: moving persistence below StartExact would let a runtime
// namespace exist without a durable generation to bind or recover it.
func TestStartPersistsGenerationBeforeSupervisorMutation(t *testing.T) {
	domainConfig, backendFake, creator := createFixture(t)
	record, err := creator.Create(context.Background(), "dev", ModeClean)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	admittedCA := sshx.CAIdentity{Domain: domainConfig.ID, Algorithm: "ssh-ed25519", PublicKey: "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGxvY2FsLWNhLXRlc3Q= boxwarden", Fingerprint: "SHA256:6w2bY4YoYMYN+d7Nr7Oe5e2eH3pi1ZCrD/bIb5A6miY"}
	var service *Service
	supervisorFake := &startSupervisorFake{start: func(request supervisor.LaunchRequest) (supervisor.Snapshot, error) {
		loaded, loadErr := LoadRecord(domainConfig.StateRoot, "work", "dev")
		if loadErr != nil {
			return supervisor.Snapshot{}, loadErr
		}
		if loaded.IntendedState != StateStarting || loaded.Readiness.Status != ReadinessStarting || loaded.StartGeneration != testStartGeneration {
			return supervisor.Snapshot{}, fmt.Errorf("durable record at first runtime mutation = %#v", loaded)
		}
		if request.Binding != (supervisor.Binding{Domain: "work", SessionID: record.ID, BackendKind: "tart", BackendObject: record.Backend.ObjectID, Generation: loaded.StartGeneration}) {
			return supervisor.Snapshot{}, fmt.Errorf("unexpected binding %#v", request.Binding)
		}
		if request.SessionRecordName != "dev" || request.RuntimeDirectory != filepath.Join(domainConfig.StateRoot, "runtime", "work", record.ID, loaded.StartGeneration) {
			return supervisor.Snapshot{}, fmt.Errorf("unexpected immutable request %#v", request)
		}
		return startedSnapshot(request.Binding, time.Now()), nil
	}}
	service = NewStartService(domainConfig, StartDependencies{
		Observer:          backendFake,
		Workspaces:        startWorkspaceFake{},
		Host:              startHostFake{},
		CA:                startCAFake{identity: admittedCA},
		Supervisor:        supervisorFake,
		RuntimeRoot:       filepath.Join(domainConfig.StateRoot, "runtime"),
		ConfigPath:        "/private/boxwarden-config.json",
		ConfiguredDomains: []sshx.Domain{{ID: domainConfig.ID, StateRoot: domainConfig.StateRoot}},
		NewGeneration:     func() (string, error) { return testStartGeneration, nil },
		Now:               time.Now,
	})

	got, err := service.Start(context.Background(), "dev")
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if got.IntendedState != StateStarting || got.StartGeneration != testStartGeneration || got.Readiness != (ReadinessRecord{Status: ReadinessStarting}) {
		t.Fatalf("Start() record = %#v, want retained durable starting generation", got)
	}
	loaded, err := LoadRecord(domainConfig.StateRoot, "work", "dev")
	if err != nil {
		t.Fatal(err)
	}
	if loaded != got {
		t.Fatalf("persisted record = %#v, want %#v", loaded, got)
	}
}

func TestStartPersistsReadyOnlyAfterFreshExactManagementEvidence(t *testing.T) {
	domainConfig, backendFake, creator := createFixture(t)
	if _, err := creator.Create(context.Background(), "dev", ModeClean); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	control := &startSupervisorFake{start: func(request supervisor.LaunchRequest) (supervisor.Snapshot, error) {
		loaded, err := LoadRecord(domainConfig.StateRoot, "work", "dev")
		if err != nil || loaded.IntendedState != StateStarting || loaded.Readiness.Status != ReadinessStarting {
			t.Fatalf("READY before durable intent: %#v, %v", loaded, err)
		}
		return readySnapshot(request.Binding, now), nil
	}}
	service := newStartTestService(domainConfig, backendFake, control, func() time.Time { return now }, func() (string, error) { return testStartGeneration, nil })
	record, err := service.Start(context.Background(), "dev")
	if err != nil || record.IntendedState != StateRunning || record.Readiness.Status != ReadinessReady {
		t.Fatalf("ready start = %#v, %v", record, err)
	}
	assertStoredRecord(t, domainConfig, record)
}

// Production break: a completed Slice C retry must reuse the exact live
// generation without asking the supervisor to bootstrap again or minting G+1.
func TestStartingRetryReusesGenerationAndClassifiesBackendBeforeMutation(t *testing.T) {
	for _, state := range []backend.ObjectState{backend.ObjectRunning, backend.ObjectStopped} {
		t.Run(string(state), func(t *testing.T) {
			domainConfig, backendFake, creator := createFixture(t)
			created, err := creator.Create(context.Background(), "dev", ModeClean)
			if err != nil {
				t.Fatal(err)
			}
			before := saveStartingRecord(t, domainConfig, created)
			backendFake.SetObservation(backend.Observation{ObjectID: created.Backend.ObjectID, Exists: true, State: state})
			now := time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)
			generationCalls := 0
			control := &startSupervisorFake{
				start: func(request supervisor.LaunchRequest) (supervisor.Snapshot, error) {
					return startedSnapshot(request.Binding, now), nil
				},
				snapshot: func(binding supervisor.Binding) (supervisor.Snapshot, error) {
					return startedSnapshot(binding, now), nil
				},
			}
			service := newStartTestService(domainConfig, backendFake, control, func() time.Time { return now }, func() (string, error) {
				generationCalls++
				return "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee", nil
			})

			got, err := service.Start(context.Background(), "dev")
			if err != nil {
				t.Fatalf("Start() error = %v", err)
			}
			if got != before {
				t.Fatalf("Start() record = %#v, want unchanged %#v", got, before)
			}
			if generationCalls != 0 {
				t.Fatalf("NewGeneration calls = %d, want 0", generationCalls)
			}
			if control.startCalls != 0 || control.snapshotCalls != 1 {
				t.Fatalf("%s retry start/snapshot calls = %d/%d, want 0/1 for exact retained owner", state, control.startCalls, control.snapshotCalls)
			}
		})
	}
}

func TestStartingRetryPrefersExactReadyOwnerWhenTartFalselyListsStopped(t *testing.T) {
	domainConfig, backendFake, creator := createFixture(t)
	created, err := creator.Create(context.Background(), "dev", ModeClean)
	if err != nil {
		t.Fatal(err)
	}
	before := saveStartingRecord(t, domainConfig, created)
	backendFake.SetObservation(backend.Observation{ObjectID: created.Backend.ObjectID, Exists: true, State: backend.ObjectStopped})
	now := time.Date(2026, 9, 24, 3, 0, 0, 0, time.UTC)
	control := &startSupervisorFake{snapshot: func(binding supervisor.Binding) (supervisor.Snapshot, error) {
		return readySnapshot(binding, now), nil
	}}
	service := newStartTestService(domainConfig, backendFake, control, func() time.Time { return now }, func() (string, error) {
		t.Fatal("false-stopped retry allocated another generation")
		return "", nil
	})
	got, err := service.Start(context.Background(), "dev")
	if err != nil || got.IntendedState != StateRunning || got.StartGeneration != before.StartGeneration || control.startCalls != 0 || control.snapshotCalls != 1 {
		t.Fatalf("false-stopped retry = %#v, %v; start/snapshot calls=%d/%d", got, err, control.startCalls, control.snapshotCalls)
	}
}

func TestStartingRetryWithStoppedListingAndNoOwnerUsesExactStartAdmission(t *testing.T) {
	domainConfig, backendFake, creator := createFixture(t)
	created, err := creator.Create(context.Background(), "dev", ModeClean)
	if err != nil {
		t.Fatal(err)
	}
	before := saveStartingRecord(t, domainConfig, created)
	backendFake.SetObservation(backend.Observation{ObjectID: created.Backend.ObjectID, Exists: true, State: backend.ObjectStopped})
	now := time.Date(2026, 9, 24, 3, 0, 0, 0, time.UTC)
	control := &startSupervisorFake{
		snapshot: func(supervisor.Binding) (supervisor.Snapshot, error) {
			return supervisor.Snapshot{}, errors.New("no exact owner")
		},
		start: func(request supervisor.LaunchRequest) (supervisor.Snapshot, error) {
			if request.Binding.Generation != before.StartGeneration {
				t.Fatal("exact start changed generation")
			}
			return startedSnapshot(request.Binding, now), nil
		},
	}
	service := newStartTestService(domainConfig, backendFake, control, func() time.Time { return now }, func() (string, error) {
		t.Fatal("stopped retry allocated another generation")
		return "", nil
	})
	got, err := service.Start(context.Background(), "dev")
	if err != nil || got != before || control.snapshotCalls != 1 || control.startCalls != 1 {
		t.Fatalf("exact start fallback = %#v, %v; snapshot/start=%d/%d", got, err, control.snapshotCalls, control.startCalls)
	}
}

func TestStartingLiveBootstrapIncompleteRetriesSameGeneration(t *testing.T) {
	domainConfig, backendFake, creator := createFixture(t)
	created, err := creator.Create(context.Background(), "dev", ModeClean)
	if err != nil {
		t.Fatal(err)
	}
	before := saveStartingRecord(t, domainConfig, created)
	backendFake.SetObservation(backend.Observation{ObjectID: created.Backend.ObjectID, Exists: true, State: backend.ObjectRunning})
	now := time.Date(2026, 9, 8, 1, 0, 0, 0, time.UTC)
	control := &startSupervisorFake{
		snapshot: func(binding supervisor.Binding) (supervisor.Snapshot, error) {
			snapshot := startedSnapshot(binding, now)
			snapshot.PinPresent = false
			return snapshot, nil
		},
		start: func(request supervisor.LaunchRequest) (supervisor.Snapshot, error) {
			if request.Binding.Generation != before.StartGeneration {
				return supervisor.Snapshot{}, fmt.Errorf("bootstrap retry changed generation")
			}
			return startedSnapshot(request.Binding, now), nil
		},
	}
	service := newStartTestService(domainConfig, backendFake, control, func() time.Time { return now }, func() (string, error) {
		t.Fatal("bootstrap retry allocated a generation")
		return "", nil
	})
	got, err := service.Start(context.Background(), "dev")
	if err != nil || got != before {
		t.Fatalf("same-G bootstrap retry = %#v, %v; want unchanged %#v", got, err, before)
	}
	if control.snapshotCalls != 1 || control.startCalls != 1 || control.stopCalls != 0 {
		t.Fatalf("snapshot/start/stop = %d/%d/%d, want 1/1/0", control.snapshotCalls, control.startCalls, control.stopCalls)
	}
}

func TestStartingPoisonedSerialStopsAndRelaunchesSameGeneration(t *testing.T) {
	domainConfig, backendFake, creator := createFixture(t)
	created, err := creator.Create(context.Background(), "dev", ModeClean)
	if err != nil {
		t.Fatal(err)
	}
	before := saveStartingRecord(t, domainConfig, created)
	backendFake.SetObservation(backend.Observation{ObjectID: created.Backend.ObjectID, Exists: true, State: backend.ObjectRunning})
	now := time.Date(2026, 9, 8, 1, 0, 0, 0, time.UTC)
	control := &startSupervisorFake{
		snapshot: func(binding supervisor.Binding) (supervisor.Snapshot, error) {
			snapshot := startedSnapshot(binding, now)
			snapshot.PinPresent = false
			snapshot.SerialHealthy = false
			return snapshot, nil
		},
		stop: func(binding supervisor.Binding) error {
			if binding.Generation != before.StartGeneration {
				return fmt.Errorf("stopped foreign generation")
			}
			ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
			defer cancel()
			held, err := lock.AcquireSession(ctx, domainConfig.StateRoot, "work", "dev")
			if err != nil {
				return fmt.Errorf("poisoned owner could not acquire session lock: %w", err)
			}
			if err := held.Release(); err != nil {
				return err
			}
			backendFake.SetObservation(backend.Observation{ObjectID: created.Backend.ObjectID, Exists: true, State: backend.ObjectStopped})
			return nil
		},
		start: func(request supervisor.LaunchRequest) (supervisor.Snapshot, error) {
			if request.Binding.Generation != before.StartGeneration {
				return supervisor.Snapshot{}, fmt.Errorf("serial recovery changed generation")
			}
			return startedSnapshot(request.Binding, now), nil
		},
	}
	service := newStartTestService(domainConfig, backendFake, control, func() time.Time { return now }, func() (string, error) {
		t.Fatal("serial recovery allocated a generation")
		return "", nil
	})
	got, err := service.Start(context.Background(), "dev")
	if err != nil || got != before {
		t.Fatalf("same-G serial recovery = %#v, %v; want unchanged %#v", got, err, before)
	}
	if control.snapshotCalls != 1 || control.stopCalls != 1 || control.startCalls != 1 {
		t.Fatalf("snapshot/stop/start = %d/%d/%d, want 1/1/1", control.snapshotCalls, control.stopCalls, control.startCalls)
	}
}

// Production break: accepting inexact live evidence could adopt another
// generation or an untrustworthy clock sample while claiming retry success.
func TestRunningBackendRetryRejectsInexactStartedSnapshotWithoutMutation(t *testing.T) {
	now := time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)
	for name, mutate := range map[string]func(*supervisor.Snapshot){
		"wrong binding": func(snapshot *supervisor.Snapshot) {
			snapshot.Binding.Generation = "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"
		},
		"stale":  func(snapshot *supervisor.Snapshot) { snapshot.ObservedAt = now.Add(-2 * time.Minute) },
		"future": func(snapshot *supervisor.Snapshot) { snapshot.ObservedAt = now.Add(time.Second) },
		"backend absent": func(snapshot *supervisor.Snapshot) {
			snapshot.BackendRunning = false
		},
	} {
		t.Run(name, func(t *testing.T) {
			domainConfig, backendFake, creator := createFixture(t)
			created, err := creator.Create(context.Background(), "dev", ModeClean)
			if err != nil {
				t.Fatal(err)
			}
			before := saveStartingRecord(t, domainConfig, created)
			backendFake.SetObservation(backend.Observation{ObjectID: created.Backend.ObjectID, Exists: true, State: backend.ObjectRunning})
			control := &startSupervisorFake{snapshot: func(binding supervisor.Binding) (supervisor.Snapshot, error) {
				snapshot := startedSnapshot(binding, now)
				mutate(&snapshot)
				return snapshot, nil
			}}
			generationCalls := 0
			service := newStartTestService(domainConfig, backendFake, control, func() time.Time { return now }, func() (string, error) {
				generationCalls++
				return "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee", nil
			})

			if _, err := service.Start(context.Background(), "dev"); err == nil {
				t.Fatal("Start() error = nil, want inexact snapshot rejection")
			}
			assertStoredRecord(t, domainConfig, before)
			if generationCalls != 0 || control.startCalls != 0 || control.snapshotCalls != 1 {
				t.Fatalf("generation/start/snapshot calls = %d/%d/%d, want 0/0/1", generationCalls, control.startCalls, control.snapshotCalls)
			}
		})
	}
}

// Production break: treating absent or internally inconsistent observation as
// stopped would authorize a retry launch without proving its exact backend.
func TestStartingRetryRejectsMissingOrAmbiguousBackendWithoutMutation(t *testing.T) {
	for name, observation := range map[string]backend.Observation{
		"missing":   {ObjectID: testObjectID, State: backend.ObjectUnknown},
		"ambiguous": {ObjectID: testObjectID, Exists: true, State: backend.ObjectUnknown},
	} {
		t.Run(name, func(t *testing.T) {
			domainConfig, backendFake, creator := createFixture(t)
			created, err := creator.Create(context.Background(), "dev", ModeClean)
			if err != nil {
				t.Fatal(err)
			}
			before := saveStartingRecord(t, domainConfig, created)
			backendFake.SetObservation(observation)
			control := &startSupervisorFake{}
			generationCalls := 0
			service := newStartTestService(domainConfig, backendFake, control, time.Now, func() (string, error) {
				generationCalls++
				return "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee", nil
			})

			if _, err := service.Start(context.Background(), "dev"); err == nil {
				t.Fatal("Start() error = nil, want backend observation rejection")
			}
			assertStoredRecord(t, domainConfig, before)
			if generationCalls != 0 || control.startCalls != 0 || control.snapshotCalls != 0 {
				t.Fatalf("generation/start/snapshot calls = %d/%d/%d, want 0/0/0", generationCalls, control.startCalls, control.snapshotCalls)
			}
		})
	}
}

// Production break: allowing a caller-provided domain subset to omit the
// selected domain would let CA admission silently fall back from its complete
// configured collection contract.
func TestStartRequiresConfiguredCACollectionToContainSelectedDomain(t *testing.T) {
	domainConfig, backendFake, creator := createFixture(t)
	if _, err := creator.Create(context.Background(), "dev", ModeClean); err != nil {
		t.Fatal(err)
	}
	service := NewStartService(domainConfig, StartDependencies{
		Observer:          backendFake,
		Workspaces:        startWorkspaceFake{},
		Host:              startHostFake{},
		CA:                startCAFake{},
		ConfiguredDomains: []sshx.Domain{{ID: "personal", StateRoot: "/private/personal"}},
		Supervisor: &startSupervisorFake{start: func(supervisor.LaunchRequest) (supervisor.Snapshot, error) {
			t.Fatal("supervisor reached without selected CA domain")
			return supervisor.Snapshot{}, nil
		}},
		RuntimeRoot:   filepath.Join(domainConfig.StateRoot, "runtime"),
		ConfigPath:    "/private/boxwarden-config.json",
		NewGeneration: func() (string, error) { return testStartGeneration, nil },
		Now:           time.Now,
	})
	if _, err := service.Start(context.Background(), "dev"); err == nil {
		t.Fatal("Start() error = nil, want selected-domain collection rejection")
	}
	loaded, err := LoadRecord(domainConfig.StateRoot, "work", "dev")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.IntendedState != StateStopped {
		t.Fatalf("record after rejected admission = %#v, want unchanged stopped intent", loaded)
	}
}

// Production break: applying the Slice B started predicate to an already
// running record would erase the future full-READY reconciliation boundary.
func TestRunningRecordStillRequiresFullFreshReadySnapshot(t *testing.T) {
	now := time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)
	for _, kind := range []string{"ready", "not-ready", "future"} {
		t.Run(kind, func(t *testing.T) {
			domainConfig, backendFake, creator := createFixture(t)
			created, err := creator.Create(context.Background(), "dev", ModeClean)
			if err != nil {
				t.Fatal(err)
			}
			before := saveRunningRecord(t, domainConfig, created)
			control := &startSupervisorFake{snapshot: func(binding supervisor.Binding) (supervisor.Snapshot, error) {
				snapshot := readySnapshot(binding, now)
				if kind == "not-ready" {
					snapshot.PinPresent = false
				}
				if kind == "future" {
					snapshot.ObservedAt = now.Add(time.Second)
				}
				return snapshot, nil
			}}
			service := newStartTestService(domainConfig, backendFake, control, func() time.Time { return now }, func() (string, error) {
				t.Fatal("running reconciliation allocated a generation")
				return "", nil
			})

			got, err := service.Start(context.Background(), "dev")
			if kind == "ready" {
				if err != nil || got != before {
					t.Fatalf("Start() = %#v, %v, want unchanged running ready", got, err)
				}
			} else {
				if err == nil {
					t.Fatal("Start() error = nil, want full ready rejection")
				}
				assertStoredRecord(t, domainConfig, before)
			}
			if control.startCalls != 0 || control.snapshotCalls != 1 {
				t.Fatalf("start/snapshot calls = %d/%d, want 0/1", control.startCalls, control.snapshotCalls)
			}
		})
	}
}

type startSupervisorFake struct {
	start          func(supervisor.LaunchRequest) (supervisor.Snapshot, error)
	snapshot       func(supervisor.Binding) (supervisor.Snapshot, error)
	ready          func(supervisor.Binding) (supervisor.Snapshot, error)
	stop           func(supervisor.Binding) error
	quiesced       func(supervisor.Binding) (bool, error)
	startCalls     int
	snapshotCalls  int
	stopCalls      int
	readyCalls     int
	lastSnapshotAt time.Time
}

func (f *startSupervisorFake) StartExact(_ context.Context, request supervisor.LaunchRequest) (supervisor.Snapshot, error) {
	f.startCalls++
	if f.start == nil {
		return supervisor.Snapshot{}, fmt.Errorf("unexpected StartExact")
	}
	return f.start(request)
}
func (f *startSupervisorFake) Snapshot(_ context.Context, binding supervisor.Binding) (supervisor.Snapshot, error) {
	f.snapshotCalls++
	if f.snapshot == nil {
		return supervisor.Snapshot{}, fmt.Errorf("unexpected Snapshot")
	}
	result, err := f.snapshot(binding)
	f.lastSnapshotAt = result.ObservedAt
	return result, err
}
func (f *startSupervisorFake) Ready(_ context.Context, binding supervisor.Binding) (supervisor.Snapshot, error) {
	f.readyCalls++
	if f.ready == nil {
		return startedSnapshot(binding, f.lastSnapshotAt), nil
	}
	return f.ready(binding)
}
func (f *startSupervisorFake) Stop(_ context.Context, binding supervisor.Binding) error {
	f.stopCalls++
	if f.stop == nil {
		return fmt.Errorf("unexpected Stop")
	}
	return f.stop(binding)
}
func (f *startSupervisorFake) Quiesced(_ context.Context, binding supervisor.Binding) (bool, error) {
	if f.quiesced == nil {
		return false, nil
	}
	return f.quiesced(binding)
}

type startHostFake struct{}

func (startHostFake) CheckRuntime(context.Context, hostx.Request) (RuntimeAdmission, error) {
	return RuntimeAdmission{}, nil
}

type startCAFake struct{ identity sshx.CAIdentity }

func (f startCAFake) Check(context.Context, sshx.Domain, []sshx.Domain) (sshx.CAIdentity, error) {
	return f.identity, nil
}

type startWorkspaceFake struct {
	prepare        func() error
	rebuildPrepare func(RebuildJournal) error
	verify         func() error
	release        func() error
}

func (f startWorkspaceFake) PrepareRebuildStart(ctx context.Context, stateRoot string, domainID domain.ID, stopped Record, generation string, observer backend.Observer, journal RebuildJournal) (Record, error) {
	if f.rebuildPrepare != nil {
		if err := f.rebuildPrepare(journal); err != nil {
			return Record{}, err
		}
	}
	return f.PrepareStart(ctx, stateRoot, domainID, stopped, generation, observer)
}

func (f startWorkspaceFake) PrepareStart(_ context.Context, stateRoot string, domainID domain.ID, stopped Record, generation string, _ backend.Observer) (Record, error) {
	if f.prepare != nil {
		if err := f.prepare(); err != nil {
			return Record{}, err
		}
	}
	started := stopped
	started.IntendedState = StateStarting
	started.StartGeneration = generation
	started.Readiness = ReadinessRecord{Status: ReadinessStarting}
	return started, SaveRecord(stateRoot, domainID, started)
}

func (f startWorkspaceFake) VerifyUses(context.Context, string, domain.ID, Record) error {
	if f.verify != nil {
		return f.verify()
	}
	return nil
}

func (f startWorkspaceFake) ReleaseUses(context.Context, string, domain.ID, Record, backend.Observer) error {
	if f.release != nil {
		return f.release()
	}
	return nil
}

func newStartTestService(domainConfig config.Domain, observer backend.Observer, control SupervisorControl, now func() time.Time, newGeneration func() (string, error)) *Service {
	return NewStartService(domainConfig, StartDependencies{
		Observer: observer, Host: startHostFake{}, CA: startCAFake{}, Workspaces: startWorkspaceFake{},
		RuntimeRoot: filepath.Join(domainConfig.StateRoot, "runtime"), ConfigPath: "/private/boxwarden-config.json",
		ConfiguredDomains: []sshx.Domain{{ID: domainConfig.ID, StateRoot: domainConfig.StateRoot}},
		Supervisor:        control, NewGeneration: newGeneration, Now: now,
	})
}

func saveStartingRecord(t *testing.T, domainConfig config.Domain, record Record) Record {
	t.Helper()
	record.IntendedState = StateStarting
	record.StartGeneration = testStartGeneration
	record.Readiness = ReadinessRecord{Status: ReadinessStarting}
	if err := SaveRecord(domainConfig.StateRoot, domainConfig.ID, record); err != nil {
		t.Fatal(err)
	}
	return record
}

func saveRunningRecord(t *testing.T, domainConfig config.Domain, record Record) Record {
	t.Helper()
	record.IntendedState = StateRunning
	record.StartGeneration = testStartGeneration
	record.Readiness = ReadinessRecord{Status: ReadinessReady}
	if err := SaveRecord(domainConfig.StateRoot, domainConfig.ID, record); err != nil {
		t.Fatal(err)
	}
	return record
}

func assertStoredRecord(t *testing.T, domainConfig config.Domain, want Record) {
	t.Helper()
	got, err := LoadRecord(domainConfig.StateRoot, string(domainConfig.ID), string(want.Name))
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("stored record = %#v, want unchanged %#v", got, want)
	}
}

func startedSnapshot(binding supervisor.Binding, now time.Time) supervisor.Snapshot {
	return supervisor.Snapshot{Binding: binding, BackendRunning: true, SerialHealthy: true, PinPresent: true, ObservedAt: now.UTC()}
}

func readySnapshot(binding supervisor.Binding, now time.Time) supervisor.Snapshot {
	snapshot := startedSnapshot(binding, now)
	snapshot.PinPresent = true
	snapshot.CertificateCurrent = true
	snapshot.ProbeOK = true
	snapshot.ZoneMatches = true
	return snapshot
}
