package alphaqual

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/weshofmann/boxwarden/internal/recipe"
	"github.com/weshofmann/boxwarden/internal/supervisor"
)

type packageReaderFunc func(context.Context, supervisor.Binding, []string) ([]supervisor.PackageVersion, error)

func (f packageReaderFunc) InspectPackages(ctx context.Context, binding supervisor.Binding, names []string) ([]supervisor.PackageVersion, error) {
	return f(ctx, binding, names)
}

func testPackageRecipe() recipe.Recipe {
	return recipe.Recipe{Version: 1,
		Source:  recipe.Source{Kind: "ubuntu-24.04.4-desktop-arm64", SHA256: "c2610520bf582976839a1724c669e1cfed0547427be5a0ad12d457b92b46ffbe"},
		Machine: recipe.Machine{CPUs: 2, MemoryMiB: 4096, SystemDiskGiB: 30}}
}

func TestPackageGuestInspectorBindsRecipeAndChunksSupervisorQueries(t *testing.T) {
	candidate, deps, _, lifecycle, _, _ := qualifierFixture(t)
	value := testPackageRecipe()
	for i := range 65 {
		value.AptPackages = append(value.AptPackages, fmt.Sprintf("pkg%02d", i))
	}
	var calls [][]string
	reader := packageReaderFunc(func(_ context.Context, binding supervisor.Binding, names []string) ([]supervisor.PackageVersion, error) {
		if binding != deps.Snapshots.(fakeSnapshots).value.Binding {
			t.Fatalf("foreign package query binding: %#v", binding)
		}
		calls = append(calls, append([]string(nil), names...))
		result := make([]supervisor.PackageVersion, 0, len(names))
		for _, name := range names {
			result = append(result, supervisor.PackageVersion{Name: name, Version: "1.0"})
		}
		return result, nil
	})
	inspector, err := NewPackageGuestInspector(value, candidate.PreparationKey, reader, func() time.Time { return deps.Now().Add(time.Second) })
	if err != nil {
		t.Fatal(err)
	}
	result, err := inspector.Inspect(context.Background(), InspectionRequest{Session: lifecycle.started, Snapshot: deps.Snapshots.(fakeSnapshots).value, PreparationKey: candidate.PreparationKey})
	if err != nil || len(result.Checks) != 1 || result.Checks[0].Name != "guest-package-inventory" || !result.Checks[0].Passed {
		t.Fatalf("inspection = %+v, %v", result, err)
	}
	if len(calls) != 3 || len(calls[0]) != 32 || len(calls[1]) != 32 || len(calls[2]) != 3 || calls[0][0] != "ubuntu-desktop" || calls[0][1] != "openssh-server" {
		t.Fatalf("package chunks = %#v", calls)
	}
	var bom struct {
		Version        int                         `json:"version"`
		PreparationKey string                      `json:"preparation_key"`
		Packages       []supervisor.PackageVersion `json:"packages"`
	}
	if err := json.Unmarshal(result.BOM, &bom); err != nil || bom.Version != 1 || bom.PreparationKey != candidate.PreparationKey || len(bom.Packages) != 67 {
		t.Fatalf("BOM = %+v, %v", bom, err)
	}
}

func TestPackageGuestInspectorRejectsWrongRecipeKeyAndPackageReport(t *testing.T) {
	candidate, deps, _, lifecycle, _, _ := qualifierFixture(t)
	value := testPackageRecipe()
	value.AptPackages = []string{"git"}
	calls := 0
	reader := packageReaderFunc(func(_ context.Context, _ supervisor.Binding, names []string) ([]supervisor.PackageVersion, error) {
		calls++
		return []supervisor.PackageVersion{{Name: "ubuntu-desktop", Version: "1"}, {Name: "openssh-server", Version: "2"}, {Name: "curl", Version: "3"}}, nil
	})
	inspector, err := NewPackageGuestInspector(value, candidate.PreparationKey, reader, deps.Now)
	if err != nil {
		t.Fatal(err)
	}
	request := InspectionRequest{Session: lifecycle.started, Snapshot: deps.Snapshots.(fakeSnapshots).value, PreparationKey: strings.Repeat("b", 64)}
	if _, err := inspector.Inspect(context.Background(), request); err == nil || calls != 0 {
		t.Fatalf("wrong recipe key reached guest: %v, calls=%d", err, calls)
	}
	request.PreparationKey = candidate.PreparationKey
	if _, err := inspector.Inspect(context.Background(), request); err == nil || calls != 1 {
		t.Fatalf("mismatched package report accepted: %v, calls=%d", err, calls)
	}
}

func TestFreshCloneQualifierAdmitsOnlyAfterExactPackageInventory(t *testing.T) {
	candidate, deps, _, _, _, _ := qualifierFixture(t)
	value := testPackageRecipe()
	value.AptPackages = []string{"git"}
	reader := packageReaderFunc(func(_ context.Context, binding supervisor.Binding, names []string) ([]supervisor.PackageVersion, error) {
		if binding != deps.Snapshots.(fakeSnapshots).value.Binding || len(names) != 3 || names[2] != "git" {
			t.Fatalf("unexpected exact package query: %#v %#v", binding, names)
		}
		return []supervisor.PackageVersion{{Name: "ubuntu-desktop", Version: "1"}, {Name: "openssh-server", Version: "2"}, {Name: "git", Version: "3"}}, nil
	})
	inspector, err := NewPackageGuestInspector(value, candidate.PreparationKey, reader, deps.Now)
	if err != nil {
		t.Fatal(err)
	}
	deps.Inspector = inspector
	qualifier, err := New(deps)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := qualifier.Qualify(context.Background(), candidate)
	if err != nil || !receipt.Passed {
		t.Fatalf("qualify inspected clone = %+v, %v", receipt, err)
	}
}
