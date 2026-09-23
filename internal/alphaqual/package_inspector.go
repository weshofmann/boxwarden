package alphaqual

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/weshofmann/boxwarden/internal/recipe"
	"github.com/weshofmann/boxwarden/internal/session"
	"github.com/weshofmann/boxwarden/internal/supervisor"
)

// PackageReader is implemented by the exact live supervisor reader. Each
// request is limited to 32 names by its private control frame contract.
type PackageReader interface {
	InspectPackages(context.Context, supervisor.Binding, []string) ([]supervisor.PackageVersion, error)
}

type PackageGuestInspector struct {
	preparationKey string
	packages       []string
	reader         PackageReader
	now            func() time.Time
}

// NewPackageGuestInspector binds the package inventory to one validated
// recipe and preparation key. It does not claim that arbitrary prepare steps
// or graphical application launch succeeded.
func NewPackageGuestInspector(value recipe.Recipe, key string, reader PackageReader, now func() time.Time) (*PackageGuestInspector, error) {
	if reader == nil || now == nil {
		return nil, errors.New("package inspector requires reader and clock")
	}
	if _, err := recipe.PreparationPayload(value, key); err != nil {
		return nil, fmt.Errorf("validate inspected recipe: %w", err)
	}
	packages := []string{"ubuntu-desktop", "openssh-server"}
	seen := map[string]bool{"ubuntu-desktop": true, "openssh-server": true}
	for _, name := range value.AptPackages {
		if !seen[name] {
			packages = append(packages, name)
			seen[name] = true
		}
	}
	return &PackageGuestInspector{preparationKey: key, packages: packages, reader: reader, now: now}, nil
}

func (i *PackageGuestInspector) Inspect(ctx context.Context, request InspectionRequest) (Inspection, error) {
	if i == nil || i.reader == nil || i.now == nil || request.PreparationKey != i.preparationKey {
		return Inspection{}, errors.New("package inspection preparation key mismatch")
	}
	sessionRecord := request.Session
	snapshot := request.Snapshot
	binding := snapshot.Binding
	if sessionRecord.Version != 2 || sessionRecord.IntendedState != session.StateRunning || sessionRecord.Readiness.Status != session.ReadinessReady ||
		string(sessionRecord.Domain) != binding.Domain || sessionRecord.ID != binding.SessionID || sessionRecord.Backend.Kind != binding.BackendKind ||
		sessionRecord.Backend.ObjectID != binding.BackendObject || sessionRecord.StartGeneration != binding.Generation ||
		!snapshot.BackendRunning || !snapshot.SerialHealthy || !snapshot.PinPresent || !snapshot.CertificateCurrent || !snapshot.ProbeOK || !snapshot.ZoneMatches {
		return Inspection{}, errors.New("package inspection has no exact ready clone")
	}
	observed := i.now().UTC()
	if snapshot.ObservedAt.IsZero() || snapshot.ObservedAt.After(observed) || observed.Sub(snapshot.ObservedAt) > maxReadyAge {
		return Inspection{}, errors.New("package inspection ready snapshot is stale")
	}
	packages := make([]supervisor.PackageVersion, 0, len(i.packages))
	for start := 0; start < len(i.packages); start += 32 {
		end := min(start+32, len(i.packages))
		chunk := i.packages[start:end]
		found, err := i.reader.InspectPackages(ctx, binding, chunk)
		if err != nil {
			return Inspection{}, fmt.Errorf("inspect exact guest packages: %w", err)
		}
		if len(found) != len(chunk) {
			return Inspection{}, errors.New("guest package inspection count mismatch")
		}
		for index, pkg := range found {
			if pkg.Name != chunk[index] || !validInspectionVersion(pkg.Version) {
				return Inspection{}, errors.New("guest package inspection differs from requested package")
			}
			packages = append(packages, pkg)
		}
	}
	finished := i.now().UTC()
	if finished.Before(observed) {
		return Inspection{}, errors.New("package inspection clock moved backwards")
	}
	bom, err := json.Marshal(struct {
		Version        int                         `json:"version"`
		PreparationKey string                      `json:"preparation_key"`
		Packages       []supervisor.PackageVersion `json:"packages"`
	}{Version: 1, PreparationKey: i.preparationKey, Packages: packages})
	if err != nil || len(bom) > maxBOMBytes-1 {
		return Inspection{}, errors.New("package inspection BOM exceeds bound")
	}
	return Inspection{Binding: binding, ObservedAt: finished, Checks: []Check{{Name: "guest-package-inventory", Passed: true}}, BOM: bom}, nil
}

func validInspectionVersion(value string) bool {
	if len(value) == 0 || len(value) > 128 {
		return false
	}
	for _, c := range value {
		if c < 0x21 || c > 0x7e {
			return false
		}
	}
	return true
}

var _ GuestInspector = (*PackageGuestInspector)(nil)
