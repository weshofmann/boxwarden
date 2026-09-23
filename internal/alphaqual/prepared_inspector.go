package alphaqual

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/weshofmann/boxwarden/internal/recipe"
	"github.com/weshofmann/boxwarden/internal/supervisor"
)

// PreparedReader is the exact-generation supervisor capability used for
// package and identity observations. Both operations retain pinned SSH.
type PreparedReader interface {
	PackageReader
	InspectIdentity(context.Context, supervisor.Binding) (supervisor.GuestIdentity, error)
}

// PreparedBaseInspector combines independent package and clone-identity
// observations. Further guest acceptance gates can extend this composition;
// the combined result is not, by itself, full alpha qualification.
type PreparedBaseInspector struct {
	packages *PackageGuestInspector
	reader   PreparedReader
	now      func() time.Time
}

func NewPreparedBaseInspector(value recipe.Recipe, key string, reader PreparedReader, now func() time.Time) (*PreparedBaseInspector, error) {
	if reader == nil || now == nil {
		return nil, errors.New("prepared base inspection requires reader and clock")
	}
	packages, err := NewPackageGuestInspector(value, key, reader, now)
	if err != nil {
		return nil, err
	}
	return &PreparedBaseInspector{packages: packages, reader: reader, now: now}, nil
}

func (i *PreparedBaseInspector) Inspect(ctx context.Context, request InspectionRequest) (Inspection, error) {
	if i == nil || i.packages == nil || i.reader == nil || i.now == nil {
		return Inspection{}, errors.New("prepared base inspector is unavailable")
	}
	packageResult, err := i.packages.Inspect(ctx, request)
	if err != nil {
		return Inspection{}, err
	}
	identity, err := i.reader.InspectIdentity(ctx, packageResult.Binding)
	if err != nil {
		return Inspection{}, fmt.Errorf("inspect exact guest identity: %w", err)
	}
	if !validPreparedIdentity(identity) {
		return Inspection{}, errors.New("fresh guest identity is invalid")
	}
	finished := i.now().UTC()
	if finished.Before(packageResult.ObservedAt) || finished.Before(request.Snapshot.ObservedAt) || finished.Sub(request.Snapshot.ObservedAt) > maxReadyAge {
		return Inspection{}, errors.New("prepared base inspection clock or ready snapshot is stale")
	}
	var software struct {
		Version        int                         `json:"version"`
		PreparationKey string                      `json:"preparation_key"`
		Packages       []supervisor.PackageVersion `json:"packages"`
	}
	if err := json.Unmarshal(packageResult.BOM, &software); err != nil || software.Version != 1 || software.PreparationKey != request.PreparationKey {
		return Inspection{}, errors.New("prepared base software BOM is invalid")
	}
	bom, err := json.Marshal(struct {
		Version        int                         `json:"version"`
		PreparationKey string                      `json:"preparation_key"`
		Identity       supervisor.GuestIdentity    `json:"identity"`
		Packages       []supervisor.PackageVersion `json:"packages"`
	}{Version: 1, PreparationKey: software.PreparationKey, Identity: identity, Packages: software.Packages})
	if err != nil || len(bom) > maxBOMBytes-1 {
		return Inspection{}, errors.New("prepared base BOM exceeds bound")
	}
	return Inspection{Binding: packageResult.Binding, ObservedAt: finished, Checks: append(packageResult.Checks, Check{Name: "fresh-guest-identity", Passed: true}), BOM: bom}, nil
}

func validPreparedIdentity(identity supervisor.GuestIdentity) bool {
	if len(identity.MachineID) != 32 || identity.MachineID == strings.Repeat("0", 32) {
		return false
	}
	for _, c := range identity.MachineID {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return identity.Hostname == "boxwarden-"+identity.MachineID[:12]
}

var _ GuestInspector = (*PreparedBaseInspector)(nil)
