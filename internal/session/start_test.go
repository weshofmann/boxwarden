package session

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/weshofmann/boxwarden/internal/hostx"
	"github.com/weshofmann/boxwarden/internal/sshx"
	"github.com/weshofmann/boxwarden/internal/supervisor"
)

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
	supervisorFake := startSupervisorFake{start: func(request supervisor.LaunchRequest) (supervisor.Snapshot, error) {
		loaded, loadErr := LoadRecord(domainConfig.StateRoot, "work", "dev")
		if loadErr != nil {
			return supervisor.Snapshot{}, loadErr
		}
		if loaded.IntendedState != StateStarting || loaded.Readiness.Status != ReadinessStarting || loaded.StartGeneration != "11111111-2222-4333-8444-555555555555" {
			return supervisor.Snapshot{}, fmt.Errorf("durable record at first runtime mutation = %#v", loaded)
		}
		if request.Binding != (supervisor.Binding{Domain: "work", SessionID: record.ID, BackendKind: "tart", BackendObject: record.Backend.ObjectID, Generation: loaded.StartGeneration}) {
			return supervisor.Snapshot{}, fmt.Errorf("unexpected binding %#v", request.Binding)
		}
		if request.SessionRecordName != "dev" || request.RuntimeDirectory != filepath.Join(domainConfig.StateRoot, "runtime", "work", record.ID, loaded.StartGeneration) {
			return supervisor.Snapshot{}, fmt.Errorf("unexpected immutable request %#v", request)
		}
		return readySnapshot(request.Binding), nil
	}}
	service = NewStartService(domainConfig, StartDependencies{
		Observer:          backendFake,
		Host:              startHostFake{},
		CA:                startCAFake{identity: admittedCA},
		Supervisor:        supervisorFake,
		RuntimeRoot:       filepath.Join(domainConfig.StateRoot, "runtime"),
		ConfigPath:        "/private/boxwarden-config.json",
		ConfiguredDomains: []sshx.Domain{{ID: domainConfig.ID, StateRoot: domainConfig.StateRoot}},
		NewGeneration:     func() (string, error) { return "11111111-2222-4333-8444-555555555555", nil },
		Now:               time.Now,
	})

	got, err := service.Start(context.Background(), "dev")
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if got.IntendedState != StateRunning || got.StartGeneration != "11111111-2222-4333-8444-555555555555" || got.Readiness != (ReadinessRecord{Status: ReadinessReady}) {
		t.Fatalf("Start() record = %#v, want durable running ready generation", got)
	}
	loaded, err := LoadRecord(domainConfig.StateRoot, "work", "dev")
	if err != nil {
		t.Fatal(err)
	}
	if loaded != got {
		t.Fatalf("persisted record = %#v, want %#v", loaded, got)
	}
	_ = service
}

// Production break: accepting a stale, incomplete, or wrong-generation
// supervisor snapshot would make durable READY a claim rather than fresh
// live runtime evidence.
func TestStartRequiresFreshExactReadySnapshotBeforeDurableReady(t *testing.T) {
	domainConfig, backendFake, creator := createFixture(t)
	if _, err := creator.Create(context.Background(), "dev", ModeClean); err != nil {
		t.Fatal(err)
	}
	service := NewStartService(domainConfig, StartDependencies{
		Observer: backendFake, Host: startHostFake{}, CA: startCAFake{},
		RuntimeRoot:       filepath.Join(domainConfig.StateRoot, "runtime"),
		ConfigPath:        "/private/boxwarden-config.json",
		ConfiguredDomains: []sshx.Domain{{ID: domainConfig.ID, StateRoot: domainConfig.StateRoot}},
		NewGeneration:     func() (string, error) { return "11111111-2222-4333-8444-555555555555", nil },
		Now:               time.Now,
		Supervisor: startSupervisorFake{start: func(binding supervisor.LaunchRequest) (supervisor.Snapshot, error) {
			snapshot := readySnapshot(binding.Binding)
			snapshot.ObservedAt = time.Now().Add(-2 * time.Minute)
			return snapshot, nil
		}},
	})

	if _, err := service.Start(context.Background(), "dev"); err == nil {
		t.Fatal("Start() error = nil, want stale snapshot rejection")
	}
	loaded, err := LoadRecord(domainConfig.StateRoot, "work", "dev")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.IntendedState != StateStarting || loaded.Readiness.Status != ReadinessStarting || loaded.StartGeneration != "11111111-2222-4333-8444-555555555555" {
		t.Fatalf("failed start record = %#v, want retained starting generation", loaded)
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
		Supervisor: startSupervisorFake{start: func(supervisor.LaunchRequest) (supervisor.Snapshot, error) {
			t.Fatal("supervisor reached without selected CA domain")
			return supervisor.Snapshot{}, nil
		}},
		RuntimeRoot:   filepath.Join(domainConfig.StateRoot, "runtime"),
		ConfigPath:    "/private/boxwarden-config.json",
		NewGeneration: func() (string, error) { return "11111111-2222-4333-8444-555555555555", nil },
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

// Production break: accepting a future-dated snapshot lets a mismatched
// supervisor clock bypass the fresh-evidence window.
func TestStartRejectsFutureReadySnapshotUsingTrustedClock(t *testing.T) {
	domainConfig, backendFake, creator := createFixture(t)
	if _, err := creator.Create(context.Background(), "dev", ModeClean); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)
	service := NewStartService(domainConfig, StartDependencies{
		Observer: backendFake, Host: startHostFake{}, CA: startCAFake{}, ConfiguredDomains: []sshx.Domain{{ID: domainConfig.ID, StateRoot: domainConfig.StateRoot}},
		RuntimeRoot: filepath.Join(domainConfig.StateRoot, "runtime"), ConfigPath: "/private/boxwarden-config.json", NewGeneration: func() (string, error) { return "11111111-2222-4333-8444-555555555555", nil }, Now: func() time.Time { return now },
		Supervisor: startSupervisorFake{start: func(request supervisor.LaunchRequest) (supervisor.Snapshot, error) {
			snapshot := readySnapshot(request.Binding)
			snapshot.ObservedAt = now.Add(time.Second)
			return snapshot, nil
		}},
	})
	if _, err := service.Start(context.Background(), "dev"); err == nil {
		t.Fatal("Start() accepted future snapshot")
	}
}

type startSupervisorFake struct {
	start func(supervisor.LaunchRequest) (supervisor.Snapshot, error)
}

func (f startSupervisorFake) StartExact(_ context.Context, request supervisor.LaunchRequest) (supervisor.Snapshot, error) {
	return f.start(request)
}
func (startSupervisorFake) Snapshot(context.Context, supervisor.Binding) (supervisor.Snapshot, error) {
	return supervisor.Snapshot{}, fmt.Errorf("not used")
}
func (startSupervisorFake) Stop(context.Context, supervisor.Binding) error { return nil }

type startHostFake struct{}

func (startHostFake) CheckRuntime(context.Context, hostx.Request) (RuntimeAdmission, error) {
	return RuntimeAdmission{}, nil
}

type startCAFake struct{ identity sshx.CAIdentity }

func (f startCAFake) Check(context.Context, sshx.Domain, []sshx.Domain) (sshx.CAIdentity, error) {
	return f.identity, nil
}

func readySnapshot(binding supervisor.Binding) supervisor.Snapshot {
	return supervisor.Snapshot{Binding: binding, BackendRunning: true, SerialHealthy: true, PinPresent: true, CertificateCurrent: true, ProbeOK: true, ZoneMatches: true, ObservedAt: time.Now().UTC()}
}
