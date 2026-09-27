package alphaqual

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/weshofmann/boxwarden/internal/backend"
	"github.com/weshofmann/boxwarden/internal/basebuild"
	"github.com/weshofmann/boxwarden/internal/domain"
	"github.com/weshofmann/boxwarden/internal/golden"
	"github.com/weshofmann/boxwarden/internal/session"
	"github.com/weshofmann/boxwarden/internal/supervisor"
)

const (
	cloneSessionID  = "00112233-4455-4677-8899-aabbccddeeff"
	cloneGeneration = "10213243-5465-4768-899a-bbccddeeff00"
	cloneObject     = "boxwarden-work-00112233445546778899aabbccddeeff"
)

type fakeRegistrar struct {
	calls  int
	record golden.Record
}

func (f *fakeRegistrar) RegisterRevision(_ context.Context, _ string) (golden.Record, error) {
	f.calls++
	return f.record, nil
}

type fakeLifecycle struct {
	created                            session.FreshCreation
	createErr                          error
	started                            session.Record
	startErr                           error
	stopped                            session.Record
	stopErr                            error
	stopErrOnce                        error
	createCalls, startCalls, stopCalls int
}

func (f *fakeLifecycle) CreateFreshFromRevision(_ context.Context, _ string, _ session.Mode, _ string) (session.FreshCreation, error) {
	f.createCalls++
	return f.created, f.createErr
}
func (f *fakeLifecycle) Start(_ context.Context, _ string) (session.Record, error) {
	f.startCalls++
	return f.started, f.startErr
}
func (f *fakeLifecycle) Stop(_ context.Context, _ string) (session.Record, error) {
	f.stopCalls++
	if f.stopErrOnce != nil {
		err := f.stopErrOnce
		f.stopErrOnce = nil
		return session.Record{}, err
	}
	return f.stopped, f.stopErr
}

type fakeObserver struct {
	observations map[string]backend.Observation
}

func (f *fakeObserver) Observe(_ context.Context, id string) (backend.Observation, error) {
	return f.observations[id], nil
}

type fakeSnapshots struct{ value supervisor.Snapshot }

func (f fakeSnapshots) Snapshot(context.Context, supervisor.Binding) (supervisor.Snapshot, error) {
	return f.value, nil
}

type fakeInspector struct {
	report  Inspection
	err     error
	calls   int
	request InspectionRequest
}

func (f *fakeInspector) Inspect(_ context.Context, request InspectionRequest) (Inspection, error) {
	f.calls++
	f.request = request
	return f.report, f.err
}

func qualifierFixture(t *testing.T) (basebuild.Result, Dependencies, *fakeRegistrar, *fakeLifecycle, *fakeObserver, *fakeInspector) {
	t.Helper()
	parent, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	attempt := filepath.Join(parent, "attempt")
	if err := os.Mkdir(attempt, 0700); err != nil {
		t.Fatal(err)
	}
	key := strings.Repeat("a", 64)
	candidate := basebuild.Result{AttemptDirectory: attempt, CandidateID: "golden-r1", PreparationKey: key, State: basebuild.CandidateStopped}
	stopped := session.Record{Version: 2, Domain: domain.ID("work"), Name: "qualclone", ID: cloneSessionID, Mode: session.ModeClean, IntendedState: session.StateStopped, Backend: session.BackendRef{Kind: "tart", ObjectID: cloneObject}, GoldenRevision: candidate.CandidateID, Readiness: session.ReadinessRecord{Status: session.ReadinessNotReady}}
	started := stopped
	started.IntendedState = session.StateRunning
	started.StartGeneration = cloneGeneration
	started.Readiness.Status = session.ReadinessReady
	binding := supervisor.Binding{Domain: "work", SessionID: cloneSessionID, BackendKind: "tart", BackendObject: cloneObject, Generation: cloneGeneration}
	now := time.Now().UTC()
	registrar := &fakeRegistrar{record: golden.Record{Version: 1, Domain: "work", Revision: candidate.CandidateID, Backend: golden.BackendRef{Kind: "tart", ObjectID: candidate.CandidateID}}}
	lifecycle := &fakeLifecycle{created: session.FreshCreation{Record: stopped, Created: true}, started: started, stopped: stopped}
	observer := &fakeObserver{observations: map[string]backend.Observation{candidate.CandidateID: {ObjectID: candidate.CandidateID, Exists: true, State: backend.ObjectStopped}, cloneObject: {ObjectID: cloneObject, Exists: true, State: backend.ObjectStopped}}}
	inspector := &fakeInspector{report: Inspection{Binding: binding, ObservedAt: now, Checks: []Check{{Name: "boot", Passed: true}}, BOM: json.RawMessage(`{"packages":[]}`)}}
	deps := Dependencies{Domain: "work", Registrar: registrar, Lifecycle: lifecycle, Observer: observer, Snapshots: fakeSnapshots{value: supervisor.Snapshot{Binding: binding, BackendRunning: true, SerialHealthy: true, PinPresent: true, CertificateCurrent: true, ProbeOK: true, ZoneMatches: true, ObservedAt: now}}, Inspector: inspector, NewName: func() (string, error) { return "qualclone", nil }, Now: func() time.Time { return now }, ACL: allowACL{}}
	return candidate, deps, registrar, lifecycle, observer, inspector
}

type allowACL struct{}

func (allowACL) HasExtendedACL(string) (bool, error) { return false, nil }

func TestQualifyRequiresFreshCloneReadyInspectionAndStoppedProof(t *testing.T) {
	candidate, deps, registrar, lifecycle, _, inspector := qualifierFixture(t)
	qualifier, err := New(deps)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := qualifier.Qualify(context.Background(), candidate)
	if err != nil {
		t.Fatal(err)
	}
	if !receipt.Passed || receipt.CloneID != cloneObject || receipt.CandidateID != candidate.CandidateID || receipt.PreparationKey != candidate.PreparationKey {
		t.Fatalf("receipt = %+v", receipt)
	}
	if registrar.calls != 1 || lifecycle.createCalls != 1 || lifecycle.startCalls != 1 || lifecycle.stopCalls != 1 || inspector.calls != 1 {
		t.Fatalf("call counts register=%d create=%d start=%d stop=%d inspect=%d", registrar.calls, lifecycle.createCalls, lifecycle.startCalls, lifecycle.stopCalls, inspector.calls)
	}
	if inspector.request.PreparationKey != candidate.PreparationKey {
		t.Fatalf("inspector preparation key = %q", inspector.request.PreparationKey)
	}
	for name, digest := range map[string]string{"qualification-evidence.json": receipt.EvidenceSHA256, "qualification-bom.json": receipt.BOMSHA256} {
		path := filepath.Join(candidate.AttemptDirectory, name)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := sha256.Sum256(data); digest != fmt.Sprintf("%x", got) {
			t.Fatalf("%s digest mismatch", name)
		}
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
			t.Fatalf("%s not private: %v %v", name, info, err)
		}
	}
}

func TestQualifyRejectsReusedCloneBeforeStartAndRetainsFailureEvidence(t *testing.T) {
	candidate, deps, _, lifecycle, _, inspector := qualifierFixture(t)
	lifecycle.created.Created = false
	qualifier, err := New(deps)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := qualifier.Qualify(context.Background(), candidate); err == nil {
		t.Fatal("reused clone qualified")
	}
	if lifecycle.startCalls != 0 || inspector.calls != 0 {
		t.Fatal("reused clone reached start or inspection")
	}
	if _, err := os.Stat(filepath.Join(candidate.AttemptDirectory, "qualification-evidence.json")); err != nil {
		t.Fatalf("failure evidence missing: %v", err)
	}
}

func TestQualifyRejectsCloneWithWrongDerivedBackendIdentity(t *testing.T) {
	candidate, deps, _, lifecycle, observer, inspector := qualifierFixture(t)
	lifecycle.created.Record.Backend.ObjectID = "unrelated-clone"
	observer.observations["unrelated-clone"] = backend.Observation{ObjectID: "unrelated-clone", Exists: true, State: backend.ObjectStopped}
	qualifier, err := New(deps)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := qualifier.Qualify(context.Background(), candidate); err == nil {
		t.Fatal("wrong clone object qualified")
	}
	if lifecycle.startCalls != 0 || inspector.calls != 0 {
		t.Fatal("wrong clone reached start or inspection")
	}
}

func TestQualifyRejectsLegacyCloneRecordBeforeStart(t *testing.T) {
	candidate, deps, _, lifecycle, _, inspector := qualifierFixture(t)
	lifecycle.created.Record.Version = 1
	qualifier, err := New(deps)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := qualifier.Qualify(context.Background(), candidate); err == nil {
		t.Fatal("legacy clone record qualified")
	}
	if lifecycle.startCalls != 0 || inspector.calls != 0 {
		t.Fatal("legacy clone reached start or inspection")
	}
}

func TestQualifyUnexpectedRunningCloneAttemptsStopBeforeLaunch(t *testing.T) {
	candidate, deps, _, lifecycle, observer, inspector := qualifierFixture(t)
	observer.observations[cloneObject] = backend.Observation{ObjectID: cloneObject, Exists: true, State: backend.ObjectRunning}
	qualifier, err := New(deps)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := qualifier.Qualify(context.Background(), candidate); err == nil {
		t.Fatal("unexpected running clone qualified")
	}
	if lifecycle.stopCalls != 1 || lifecycle.startCalls != 0 || inspector.calls != 0 {
		t.Fatalf("stop=%d start=%d inspect=%d", lifecycle.stopCalls, lifecycle.startCalls, inspector.calls)
	}
}

func TestQualifyRejectsStaleReadyAndStopsClone(t *testing.T) {
	candidate, deps, _, lifecycle, _, inspector := qualifierFixture(t)
	snap := deps.Snapshots.(fakeSnapshots)
	snap.value.ObservedAt = deps.Now().Add(-2 * time.Minute)
	deps.Snapshots = snap
	qualifier, err := New(deps)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := qualifier.Qualify(context.Background(), candidate); err == nil {
		t.Fatal("stale READY qualified")
	}
	if lifecycle.stopCalls != 1 || inspector.calls != 0 {
		t.Fatalf("cleanup stop=%d inspector=%d", lifecycle.stopCalls, inspector.calls)
	}
}

func TestQualifyInspectionFailureRetainsCloneAndReportsStopFailure(t *testing.T) {
	candidate, deps, _, lifecycle, _, inspector := qualifierFixture(t)
	inspector.err = errors.New("guest check failed")
	lifecycle.stopErr = errors.New("stop failed")
	qualifier, err := New(deps)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := qualifier.Qualify(context.Background(), candidate); err == nil || !strings.Contains(err.Error(), "stop failed") {
		t.Fatalf("qualification error = %v", err)
	}
	if lifecycle.stopCalls != 1 {
		t.Fatalf("cleanup stop calls = %d", lifecycle.stopCalls)
	}
	data, err := os.ReadFile(filepath.Join(candidate.AttemptDirectory, "qualification-evidence.json"))
	if err != nil || !strings.Contains(string(data), `"passed":false`) {
		t.Fatalf("failure evidence = %q, %v", data, err)
	}
}

func TestQualifyGuestFailureRecordsExactCleanupStop(t *testing.T) {
	candidate, deps, _, lifecycle, _, inspector := qualifierFixture(t)
	inspector.err = errors.New("guest check failed")
	qualifier, err := New(deps)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := qualifier.Qualify(context.Background(), candidate); err == nil {
		t.Fatal("failed guest check qualified")
	}
	if lifecycle.stopCalls != 1 {
		t.Fatalf("cleanup stop calls = %d", lifecycle.stopCalls)
	}
	data, err := os.ReadFile(filepath.Join(candidate.AttemptDirectory, "qualification-evidence.json"))
	if err != nil {
		t.Fatal(err)
	}
	var evidence struct {
		StopProven bool `json:"stop_proven"`
	}
	if err := json.Unmarshal(data, &evidence); err != nil || !evidence.StopProven {
		t.Fatalf("cleanup stop not proven in evidence: %s, %v", data, err)
	}
}

func TestQualifyRetriesTransientPublicStopForContainment(t *testing.T) {
	candidate, deps, _, lifecycle, _, _ := qualifierFixture(t)
	lifecycle.stopErrOnce = errors.New("transient public stop failure")
	qualifier, err := New(deps)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := qualifier.Qualify(context.Background(), candidate); err == nil || !strings.Contains(err.Error(), "transient public stop failure") {
		t.Fatalf("qualification should fail after uncertain stop: %v", err)
	}
	if lifecycle.stopCalls != 2 {
		t.Fatalf("stop calls = %d, want bounded cleanup retry", lifecycle.stopCalls)
	}
	data, err := os.ReadFile(filepath.Join(candidate.AttemptDirectory, "qualification-evidence.json"))
	if err != nil || !strings.Contains(string(data), `"stop_proven":true`) {
		t.Fatalf("cleanup stop evidence = %s, %v", data, err)
	}
}

func TestQualifyRejectsGuestReportFromDifferentGeneration(t *testing.T) {
	candidate, deps, _, lifecycle, _, inspector := qualifierFixture(t)
	inspector.report.Binding.Generation = "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"
	qualifier, err := New(deps)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := qualifier.Qualify(context.Background(), candidate); err == nil {
		t.Fatal("wrong guest generation qualified")
	}
	if lifecycle.stopCalls != 1 {
		t.Fatalf("cleanup stop calls = %d", lifecycle.stopCalls)
	}
}
