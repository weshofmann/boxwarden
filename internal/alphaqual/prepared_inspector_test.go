package alphaqual

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/weshofmann/boxwarden/internal/supervisor"
)

type preparedReader struct {
	identity                    supervisor.GuestIdentity
	packageCalls, identityCalls int
	binding                     supervisor.Binding
}

func (r *preparedReader) InspectPackages(_ context.Context, binding supervisor.Binding, names []string) ([]supervisor.PackageVersion, error) {
	r.packageCalls++
	r.binding = binding
	result := make([]supervisor.PackageVersion, len(names))
	for index, name := range names {
		result[index] = supervisor.PackageVersion{Name: name, Version: "1.0"}
	}
	return result, nil
}

func (r *preparedReader) InspectIdentity(_ context.Context, binding supervisor.Binding) (supervisor.GuestIdentity, error) {
	r.identityCalls++
	r.binding = binding
	return r.identity, nil
}

func TestPreparedInspectorRequiresBothPackageAndFreshIdentityReports(t *testing.T) {
	candidate, deps, _, lifecycle, _, _ := qualifierFixture(t)
	machineID := strings.Repeat("b", 32)
	reader := &preparedReader{identity: supervisor.GuestIdentity{MachineID: machineID, Hostname: "boxwarden-" + machineID[:12]}}
	inspector, err := NewPreparedBaseInspector(testPackageRecipe(), candidate.PreparationKey, reader, deps.Now)
	if err != nil {
		t.Fatal(err)
	}
	request := InspectionRequest{Session: lifecycle.started, Snapshot: deps.Snapshots.(fakeSnapshots).value, PreparationKey: candidate.PreparationKey}
	result, err := inspector.Inspect(context.Background(), request)
	if err != nil || reader.packageCalls != 1 || reader.identityCalls != 1 || reader.binding != request.Snapshot.Binding {
		t.Fatalf("prepared inspection = %+v, %v; reader = %+v", result, err, reader)
	}
	if len(result.Checks) != 2 || result.Checks[0].Name != "guest-package-inventory" || result.Checks[1].Name != "fresh-guest-identity" {
		t.Fatalf("checks = %+v", result.Checks)
	}
	var bom struct {
		Version        int                         `json:"version"`
		PreparationKey string                      `json:"preparation_key"`
		Identity       supervisor.GuestIdentity    `json:"identity"`
		Packages       []supervisor.PackageVersion `json:"packages"`
	}
	if err := json.Unmarshal(result.BOM, &bom); err != nil || bom.Version != 1 || bom.PreparationKey != candidate.PreparationKey || bom.Identity != reader.identity || len(bom.Packages) != 2 {
		t.Fatalf("BOM = %+v, %v", bom, err)
	}
	reader.identity.Hostname = "boxwarden-task0-run-1"
	if _, err := inspector.Inspect(context.Background(), request); err == nil {
		t.Fatal("stale effective identity report accepted")
	}
	reader.identity = supervisor.GuestIdentity{MachineID: strings.Repeat("0", 32), Hostname: "boxwarden-000000000000"}
	if _, err := inspector.Inspect(context.Background(), request); err == nil {
		t.Fatal("uninitialized machine ID accepted")
	}
	request.PreparationKey = strings.Repeat("c", 64)
	before := reader.packageCalls
	if _, err := inspector.Inspect(context.Background(), request); err == nil || reader.packageCalls != before {
		t.Fatal("wrong preparation key reached guest")
	}
}

func TestPreparedInspectorRejectsClockRollback(t *testing.T) {
	candidate, deps, _, lifecycle, _, _ := qualifierFixture(t)
	machineID := strings.Repeat("b", 32)
	reader := &preparedReader{identity: supervisor.GuestIdentity{MachineID: machineID, Hostname: "boxwarden-" + machineID[:12]}}
	clockCalls := 0
	clock := func() time.Time {
		clockCalls++
		if clockCalls > 2 {
			return deps.Now().Add(-time.Second)
		}
		return deps.Now()
	}
	inspector, err := NewPreparedBaseInspector(testPackageRecipe(), candidate.PreparationKey, reader, clock)
	if err != nil {
		t.Fatal(err)
	}
	request := InspectionRequest{Session: lifecycle.started, Snapshot: deps.Snapshots.(fakeSnapshots).value, PreparationKey: candidate.PreparationKey}
	if _, err := inspector.Inspect(context.Background(), request); err == nil {
		t.Fatal("clock rollback after identity query accepted")
	}
}

func TestFreshCloneQualificationStopsWhenIdentityInspectionFails(t *testing.T) {
	candidate, deps, _, lifecycle, _, _ := qualifierFixture(t)
	machineID := strings.Repeat("b", 32)
	reader := &preparedReader{identity: supervisor.GuestIdentity{MachineID: machineID, Hostname: "boxwarden-task0-run-1"}}
	inspector, err := NewPreparedBaseInspector(testPackageRecipe(), candidate.PreparationKey, reader, deps.Now)
	if err != nil {
		t.Fatal(err)
	}
	deps.Inspector = inspector
	qualifier, err := New(deps)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := qualifier.Qualify(context.Background(), candidate); err == nil || lifecycle.stopCalls != 1 {
		t.Fatalf("invalid identity did not stop qualification clone: %v, stops=%d", err, lifecycle.stopCalls)
	}
}

func TestFreshCloneQualificationRecordsPreparedInspection(t *testing.T) {
	candidate, deps, _, lifecycle, _, _ := qualifierFixture(t)
	machineID := strings.Repeat("b", 32)
	reader := &preparedReader{identity: supervisor.GuestIdentity{MachineID: machineID, Hostname: "boxwarden-" + machineID[:12]}}
	inspector, err := NewPreparedBaseInspector(testPackageRecipe(), candidate.PreparationKey, reader, deps.Now)
	if err != nil {
		t.Fatal(err)
	}
	deps.Inspector = inspector
	qualifier, err := New(deps)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := qualifier.Qualify(context.Background(), candidate)
	if err != nil || !receipt.Passed || lifecycle.stopCalls != 1 || reader.packageCalls != 1 || reader.identityCalls != 1 {
		t.Fatalf("qualification = %+v, %v; stops=%d package=%d identity=%d", receipt, err, lifecycle.stopCalls, reader.packageCalls, reader.identityCalls)
	}
}
