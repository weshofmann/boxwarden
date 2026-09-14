package session

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/weshofmann/boxwarden/internal/backend"
	"github.com/weshofmann/boxwarden/internal/config"
	"github.com/weshofmann/boxwarden/internal/hostx"
	"github.com/weshofmann/boxwarden/internal/sshx"
	"github.com/weshofmann/boxwarden/internal/supervisor"
)

const testStartGeneration = "11111111-2222-4333-8444-555555555555"

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
			if state == backend.ObjectRunning && (control.startCalls != 0 || control.snapshotCalls != 1) {
				t.Fatalf("running retry start/snapshot calls = %d/%d, want 0/1", control.startCalls, control.snapshotCalls)
			}
			if state == backend.ObjectStopped && (control.startCalls != 1 || control.snapshotCalls != 0) {
				t.Fatalf("stopped retry start/snapshot calls = %d/%d, want 1/0", control.startCalls, control.snapshotCalls)
			}
		})
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
	start         func(supervisor.LaunchRequest) (supervisor.Snapshot, error)
	snapshot      func(supervisor.Binding) (supervisor.Snapshot, error)
	stop          func(supervisor.Binding) error
	startCalls    int
	snapshotCalls int
	stopCalls     int
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
	return f.snapshot(binding)
}
func (f *startSupervisorFake) Stop(_ context.Context, binding supervisor.Binding) error {
	f.stopCalls++
	if f.stop == nil {
		return fmt.Errorf("unexpected Stop")
	}
	return f.stop(binding)
}

type startHostFake struct{}

func (startHostFake) CheckRuntime(context.Context, hostx.Request) (RuntimeAdmission, error) {
	return RuntimeAdmission{}, nil
}

type startCAFake struct{ identity sshx.CAIdentity }

func (f startCAFake) Check(context.Context, sshx.Domain, []sshx.Domain) (sshx.CAIdentity, error) {
	return f.identity, nil
}

func newStartTestService(domainConfig config.Domain, observer backend.Observer, control SupervisorControl, now func() time.Time, newGeneration func() (string, error)) *Service {
	return NewStartService(domainConfig, StartDependencies{
		Observer: observer, Host: startHostFake{}, CA: startCAFake{},
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
