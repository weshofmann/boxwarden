package session

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/weshofmann/boxwarden/internal/backend"
	"github.com/weshofmann/boxwarden/internal/backend/fake"
	"github.com/weshofmann/boxwarden/internal/config"
	"github.com/weshofmann/boxwarden/internal/domain"
	"github.com/weshofmann/boxwarden/internal/golden"
	"github.com/weshofmann/boxwarden/internal/lock"
)

const (
	testSessionID = "00112233-4455-4677-8899-aabbccddeeff"
	testObjectID  = "boxwarden-work-00112233445546778899aabbccddeeff"
)

func TestCreatePersistsIntentBeforeMutationAndReturnsStoppedClone(t *testing.T) {
	domainConfig, backendFake, service := createFixture(t)
	service.hook = func(stage createStage, record Record) error {
		if stage != createAfterIntent {
			return nil
		}
		loaded, err := LoadRecord(domainConfig.StateRoot, "work", "dev")
		if err != nil {
			t.Fatalf("LoadRecord at intent hook: %v", err)
		}
		if loaded != record || loaded.IntendedState != StateCreating {
			t.Fatalf("record at intent hook = %#v, want creating %#v", loaded, record)
		}
		if len(backendFake.CloneCalls()) != 0 || len(backendFake.RandomizeMACCalls()) != 0 {
			t.Fatal("backend mutated before creating intent was durable")
		}
		return nil
	}

	record, err := service.Create(context.Background(), "dev", ModeClean)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if record.Version != recordVersion || record.ID != testSessionID || record.Backend.ObjectID != testObjectID || record.GoldenRevision != "golden-r1" || record.IntendedState != StateStopped || record.StartGeneration != "" || record.Readiness != (ReadinessRecord{Status: ReadinessNotReady}) {
		t.Fatalf("Create() record = %#v, want stopped UUID-derived golden clone", record)
	}
	if got, want := backendFake.CloneCalls(), []fake.CloneCall{{SourceID: "golden-r1", TargetID: testObjectID}}; !equalCloneCalls(got, want) {
		t.Fatalf("CloneCalls() = %#v, want %#v", got, want)
	}
	if got, want := backendFake.RandomizeMACCalls(), []string{testObjectID}; !equalStrings(got, want) {
		t.Fatalf("RandomizeMACCalls() = %#v, want %#v", got, want)
	}
	loaded, err := LoadRecord(domainConfig.StateRoot, "work", "dev")
	if err != nil {
		t.Fatalf("LoadRecord final: %v", err)
	}
	if loaded != record {
		t.Fatalf("LoadRecord final = %#v, want %#v", loaded, record)
	}
}

func TestCreateUpgradesStoppedVersion1RecordAndReturnsPersistedVersion2(t *testing.T) {
	domainConfig, backendFake, service := createFixture(t)
	if err := os.Mkdir(filepath.Join(domainConfig.StateRoot, "sessions"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeRecord(t, domainConfig.StateRoot, "dev", `{"version":1,"domain":"work","name":"dev","id":"00112233-4455-4677-8899-aabbccddeeff","mode":"clean","intended_state":"stopped","backend":{"kind":"tart","object_id":"boxwarden-work-00112233445546778899aabbccddeeff"},"golden_revision":"golden-r1"}`)
	backendFake.SetObservation(backend.Observation{ObjectID: testObjectID, Exists: true, State: backend.ObjectStopped})

	record, err := service.Create(context.Background(), "dev", ModeClean)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	loaded, err := LoadRecord(domainConfig.StateRoot, "work", "dev")
	if err != nil {
		t.Fatal(err)
	}
	if record != loaded || record.Version != recordVersion || record.Readiness != (ReadinessRecord{Status: ReadinessNotReady}) {
		t.Fatalf("Create() record = %#v, persisted = %#v, want returned version 2 not-ready record", record, loaded)
	}
	if len(backendFake.CloneCalls()) != 0 || len(backendFake.RandomizeMACCalls()) != 0 {
		t.Fatal("stopped version 1 retry repeated backend mutation")
	}
}

func TestCreateRejectsMissingOrNoLongerStoppedRegisteredGolden(t *testing.T) {
	for name, prepare := range map[string]func(t *testing.T, domainConfig config.Domain, backendFake *fake.Backend){
		"missing registration": func(_ *testing.T, _ config.Domain, _ *fake.Backend) {},
		"registered golden now running": func(t *testing.T, domainConfig config.Domain, backendFake *fake.Backend) {
			registerFixtureGolden(t, domainConfig, backendFake)
			backendFake.SetObservation(backend.Observation{ObjectID: "golden-r1", Exists: true, State: backend.ObjectRunning})
		},
		"registered golden now missing": func(t *testing.T, domainConfig config.Domain, backendFake *fake.Backend) {
			registerFixtureGolden(t, domainConfig, backendFake)
			backendFake.DeleteObservation("golden-r1")
		},
		"registered golden now ambiguous": func(t *testing.T, domainConfig config.Domain, backendFake *fake.Backend) {
			registerFixtureGolden(t, domainConfig, backendFake)
			backendFake.SetObservation(backend.Observation{ObjectID: "golden-r1", Exists: false, State: backend.ObjectStopped})
		},
	} {
		t.Run(name, func(t *testing.T) {
			domainConfig := bareDomain(t)
			backendFake := fake.New()
			prepare(t, domainConfig, backendFake)
			service := NewService(domainConfig, backendFake, backendFake)
			service.newID = func() (string, error) { return testSessionID, nil }
			if _, err := service.Create(context.Background(), "dev", ModeClean); err == nil {
				t.Fatal("Create() error = nil, want golden rejection")
			}
			if _, err := LoadRecord(domainConfig.StateRoot, "work", "dev"); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("LoadRecord() error = %v, want no session intent", err)
			}
			if len(backendFake.CloneCalls()) != 0 || len(backendFake.RandomizeMACCalls()) != 0 {
				t.Fatal("unavailable golden caused backend mutation")
			}
		})
	}
}

func TestCreateFailureAfterCloneRetriesWithoutDuplicateClone(t *testing.T) {
	_, backendFake, service := createFixture(t)
	wantErr := errors.New("interrupted after clone")
	service.hook = failCreateAt(createAfterClone, wantErr)
	if _, err := service.Create(context.Background(), "dev", ModeClean); !errors.Is(err, wantErr) {
		t.Fatalf("first Create() error = %v, want %v", err, wantErr)
	}
	assertStoredState(t, service.domain, "dev", StateCreating)
	service.hook = nil

	record, err := service.Create(context.Background(), "dev", ModeClean)
	if err != nil {
		t.Fatalf("retry Create() error = %v", err)
	}
	if record.IntendedState != StateStopped {
		t.Fatalf("retry state = %q, want stopped", record.IntendedState)
	}
	if got := len(backendFake.CloneCalls()); got != 1 {
		t.Fatalf("clone attempts = %d, want 1", got)
	}
	if got := len(backendFake.RandomizeMACCalls()); got != 1 {
		t.Fatalf("MAC attempts = %d, want 1", got)
	}
}

func TestCreateClonePostEffectErrorRetriesWithoutSecondClone(t *testing.T) {
	_, backendFake, service := createFixture(t)
	wantErr := errors.New("Tart response lost after clone")
	backendFake.SetClonePostEffectError(wantErr)
	if _, err := service.Create(context.Background(), "dev", ModeClean); !errors.Is(err, wantErr) {
		t.Fatalf("first Create() error = %v, want %v", err, wantErr)
	}
	assertStoredState(t, service.domain, "dev", StateCreating)
	backendFake.SetClonePostEffectError(nil)

	if _, err := service.Create(context.Background(), "dev", ModeClean); err != nil {
		t.Fatalf("retry Create() error = %v", err)
	}
	if got := len(backendFake.CloneCalls()); got != 1 {
		t.Fatalf("clone attempts = %d, want 1 after post-effect error", got)
	}
}

func TestCreateFailureAfterMACRetriesWithoutDuplicateClone(t *testing.T) {
	_, backendFake, service := createFixture(t)
	wantErr := errors.New("interrupted after MAC")
	service.hook = failCreateAt(createAfterMAC, wantErr)
	if _, err := service.Create(context.Background(), "dev", ModeClean); !errors.Is(err, wantErr) {
		t.Fatalf("first Create() error = %v, want %v", err, wantErr)
	}
	assertStoredState(t, service.domain, "dev", StateCreating)
	service.hook = nil

	if _, err := service.Create(context.Background(), "dev", ModeClean); err != nil {
		t.Fatalf("retry Create() error = %v", err)
	}
	if got := len(backendFake.CloneCalls()); got != 1 {
		t.Fatalf("clone attempts = %d, want 1", got)
	}
	if got := len(backendFake.RandomizeMACCalls()); got != 2 {
		t.Fatalf("MAC attempts = %d, want 2 because an interrupted MAC mutation has no observable completion marker", got)
	}
}

func TestCreateInterruptionAfterIntentRetriesExactReservedIdentity(t *testing.T) {
	_, backendFake, service := createFixture(t)
	wantErr := errors.New("interrupted after intent")
	service.hook = failCreateAt(createAfterIntent, wantErr)
	if _, err := service.Create(context.Background(), "dev", ModeClean); !errors.Is(err, wantErr) {
		t.Fatalf("first Create() error = %v, want %v", err, wantErr)
	}
	creating := assertStoredState(t, service.domain, "dev", StateCreating)
	service.hook = nil
	service.newID = func() (string, error) { return "ffffffff-ffff-4fff-bfff-ffffffffffff", nil }

	record, err := service.Create(context.Background(), "dev", ModeClean)
	if err != nil {
		t.Fatalf("retry Create() error = %v", err)
	}
	if record.ID != creating.ID || record.Backend.ObjectID != creating.Backend.ObjectID {
		t.Fatalf("retry identity = %#v, want retained intent %#v", record, creating)
	}
	if got := backendFake.CloneCalls(); len(got) != 1 || got[0].TargetID != creating.Backend.ObjectID {
		t.Fatalf("CloneCalls() = %#v, want exact reserved target", got)
	}
}

func TestCreateResyncsLoadedCreatingIntentBeforeBackendMutation(t *testing.T) {
	domainConfig, backendFake, service := createFixture(t)
	record := Record{
		Version:        recordVersion,
		Domain:         domainConfig.ID,
		Name:           Name("dev"),
		ID:             testSessionID,
		Mode:           ModeClean,
		IntendedState:  StateCreating,
		Backend:        BackendRef{Kind: "tart", ObjectID: testObjectID},
		GoldenRevision: "golden-r1",
		Readiness:      ReadinessRecord{Status: ReadinessNotReady},
	}
	interruptedSync := errors.New("directory sync interrupted")
	if err := saveRecord(domainConfig.StateRoot, domainConfig.ID, record, func(stage storeStage) error {
		if stage == storeAfterRename {
			return interruptedSync
		}
		return nil
	}); !errors.Is(err, interruptedSync) {
		t.Fatalf("seed saveRecord() error = %v, want %v", err, interruptedSync)
	}

	oldSync := sessionSyncRoot
	defer func() { sessionSyncRoot = oldSync }()
	syncCalls := 0
	sessionSyncRoot = func(root *os.Root) error {
		syncCalls++
		return syncSessionRootDirectory(root)
	}
	wantStop := errors.New("stop after checking durable intent")
	backendFake.SetCloneFault(func(_ context.Context, _ fake.CloneCall) error {
		if syncCalls == 0 {
			return errors.New("backend mutation preceded intent directory sync")
		}
		return wantStop
	})

	if _, err := service.Create(context.Background(), "dev", ModeClean); !errors.Is(err, wantStop) {
		t.Fatalf("Create() error = %v, want backend reached only after durable intent (%v)", err, wantStop)
	}
}

func TestCreateRetainsIntentAcrossBackendFailures(t *testing.T) {
	for name, configure := range map[string]func(*fake.Backend){
		"clone failure": func(backendFake *fake.Backend) {
			backendFake.SetCloneError(errors.New("clone unavailable"))
		},
		"MAC failure": func(backendFake *fake.Backend) {
			backendFake.SetRandomizeMACError(errors.New("MAC unavailable"))
		},
	} {
		t.Run(name, func(t *testing.T) {
			_, _, service := createFixture(t)
			configure(service.creator.(*fake.Backend))
			if _, err := service.Create(context.Background(), "dev", ModeClean); err == nil {
				t.Fatal("Create() error = nil, want backend failure")
			}
			assertStoredState(t, service.domain, "dev", StateCreating)
		})
	}
}

func TestCreateRejectsCollisionBeforePersistingIntent(t *testing.T) {
	domainConfig, backendFake, service := createFixture(t)
	backendFake.SetObservation(backend.Observation{ObjectID: testObjectID, Exists: true, State: backend.ObjectStopped})

	if _, err := service.Create(context.Background(), "dev", ModeClean); err == nil || !strings.Contains(err.Error(), "collision") {
		t.Fatalf("Create() error = %v, want collision rejection", err)
	}
	if _, err := LoadRecord(domainConfig.StateRoot, "work", "dev"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("LoadRecord() error = %v, want no persisted session", err)
	}
	if len(backendFake.CloneCalls()) != 0 || len(backendFake.RandomizeMACCalls()) != 0 {
		t.Fatal("collision caused backend mutation")
	}
}

func TestCreateRejectsIdentityReservedByAnotherPendingSession(t *testing.T) {
	domainConfig, backendFake, service := createFixture(t)
	wantErr := errors.New("interrupt first reservation")
	service.hook = failCreateAt(createAfterIntent, wantErr)
	if _, err := service.Create(context.Background(), "dev", ModeClean); !errors.Is(err, wantErr) {
		t.Fatalf("Create(dev) error = %v, want %v", err, wantErr)
	}
	service.hook = nil

	if _, err := service.Create(context.Background(), "ops", ModeClean); err == nil || !strings.Contains(err.Error(), "reserved") {
		t.Fatalf("Create(ops) error = %v, want existing intent reservation rejection", err)
	}
	if _, err := LoadRecord(domainConfig.StateRoot, "work", "ops"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("LoadRecord(ops) error = %v, want no duplicate reservation", err)
	}
	if len(backendFake.CloneCalls()) != 0 || len(backendFake.RandomizeMACCalls()) != 0 {
		t.Fatal("duplicate pending reservation caused backend mutation")
	}
}

func TestCreateRetryUsesRecordedGoldenAfterCurrentPointerChanges(t *testing.T) {
	domainConfig, backendFake, service := createFixture(t)
	wantErr := errors.New("interrupted after intent")
	service.hook = failCreateAt(createAfterIntent, wantErr)
	if _, err := service.Create(context.Background(), "dev", ModeClean); !errors.Is(err, wantErr) {
		t.Fatalf("first Create() error = %v, want %v", err, wantErr)
	}
	backendFake.SetObservation(backend.Observation{ObjectID: "golden-r2", Exists: true, State: backend.ObjectStopped})
	if _, err := golden.Register(context.Background(), domainConfig, "golden-r2", backendFake); err != nil {
		t.Fatalf("Register golden-r2: %v", err)
	}
	service.hook = nil

	record, err := service.Create(context.Background(), "dev", ModeClean)
	if err != nil {
		t.Fatalf("retry Create() error = %v", err)
	}
	if record.GoldenRevision != "golden-r1" {
		t.Fatalf("retry GoldenRevision = %q, want retained golden-r1", record.GoldenRevision)
	}
	if got := backendFake.CloneCalls(); len(got) != 1 || got[0].SourceID != "golden-r1" {
		t.Fatalf("CloneCalls() = %#v, want recorded golden-r1", got)
	}
}

func TestCreateIsIdempotentForConsistentStoppedRecord(t *testing.T) {
	_, backendFake, service := createFixture(t)
	first, err := service.Create(context.Background(), "dev", ModeClean)
	if err != nil {
		t.Fatalf("first Create() error = %v", err)
	}
	second, err := service.Create(context.Background(), "dev", ModeClean)
	if err != nil {
		t.Fatalf("second Create() error = %v", err)
	}
	if second != first {
		t.Fatalf("second Create() = %#v, want %#v", second, first)
	}
	if len(backendFake.CloneCalls()) != 1 || len(backendFake.RandomizeMACCalls()) != 1 {
		t.Fatal("idempotent stopped retry repeated backend mutation")
	}
}

func TestCreateRejectsModeChangeAndBackendDrift(t *testing.T) {
	for name, test := range map[string]struct {
		mutate func(*fake.Backend)
		mode   Mode
	}{
		"mode change": {
			mutate: func(*fake.Backend) {},
			mode:   ModeQuarantine,
		},
		"running drift": {
			mutate: func(backendFake *fake.Backend) {
				backendFake.SetObservation(backend.Observation{ObjectID: testObjectID, Exists: true, State: backend.ObjectRunning})
			},
			mode: ModeClean,
		},
		"missing drift": {
			mutate: func(backendFake *fake.Backend) {
				backendFake.DeleteObservation(testObjectID)
			},
			mode: ModeClean,
		},
	} {
		t.Run(name, func(t *testing.T) {
			_, backendFake, service := createFixture(t)
			if _, err := service.Create(context.Background(), "dev", ModeClean); err != nil {
				t.Fatalf("initial Create() error = %v", err)
			}
			test.mutate(backendFake)
			if _, err := service.Create(context.Background(), "dev", test.mode); err == nil {
				t.Fatal("Create() error = nil, want existing-record rejection")
			}
		})
	}
}

func TestCreateHonorsSessionLockContention(t *testing.T) {
	domainConfig, _, service := createFixture(t)
	held, err := lock.AcquireSession(context.Background(), domainConfig.StateRoot, "work", "dev")
	if err != nil {
		t.Fatalf("AcquireSession: %v", err)
	}
	defer held.Release()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := service.Create(ctx, "dev", ModeClean); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Create() error = %v, want session lock deadline", err)
	}
}

func createFixture(t *testing.T) (config.Domain, *fake.Backend, *Service) {
	t.Helper()
	domainConfig := bareDomain(t)
	backendFake := fake.New(backend.Observation{ObjectID: "golden-r1", Exists: true, State: backend.ObjectStopped})
	if _, err := golden.Register(context.Background(), domainConfig, "golden-r1", backendFake); err != nil {
		t.Fatalf("Register golden-r1: %v", err)
	}
	service := NewService(domainConfig, backendFake, backendFake)
	service.newID = func() (string, error) { return testSessionID, nil }
	return domainConfig, backendFake, service
}

func bareDomain(t *testing.T) config.Domain {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	domainID, err := domain.Parse("work")
	if err != nil {
		t.Fatal(err)
	}
	domainConfig := config.Domain{ID: domainID, StateRoot: root}
	return domainConfig
}

func registerFixtureGolden(t *testing.T, domainConfig config.Domain, backendFake *fake.Backend) {
	t.Helper()
	backendFake.SetObservation(backend.Observation{ObjectID: "golden-r1", Exists: true, State: backend.ObjectStopped})
	if _, err := golden.Register(context.Background(), domainConfig, "golden-r1", backendFake); err != nil {
		t.Fatalf("Register golden-r1: %v", err)
	}
}

func failCreateAt(want createStage, wantErr error) createHook {
	return func(got createStage, _ Record) error {
		if got == want {
			return wantErr
		}
		return nil
	}
}

func assertStoredState(t *testing.T, domainConfig config.Domain, name string, state IntendedState) Record {
	t.Helper()
	record, err := LoadRecord(domainConfig.StateRoot, string(domainConfig.ID), name)
	if err != nil {
		t.Fatalf("LoadRecord(%s): %v", name, err)
	}
	if record.IntendedState != state {
		t.Fatalf("stored state = %q, want %q", record.IntendedState, state)
	}
	return record
}

func equalCloneCalls(got, want []fake.CloneCall) bool {
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

func equalStrings(got, want []string) bool {
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
