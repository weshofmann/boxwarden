package alphaprep

import (
	"context"
	"errors"
	"testing"

	"github.com/weshofmann/boxwarden/internal/alphaqual"
	"github.com/weshofmann/boxwarden/internal/backend/fake"
)

type unavailableGuestInspector struct{}

func (unavailableGuestInspector) Inspect(context.Context, alphaqual.InspectionRequest) (alphaqual.Inspection, error) {
	return alphaqual.Inspection{}, errors.New("guest acceptance unavailable")
}

func TestNewProductionQualifierRequiresExactAdmittedDomainAndInspector(t *testing.T) {
	loaded, selected := preflightFixture(t)
	backend := fake.New()
	path := "/private/qualified/config.json"
	qualifier, err := NewProductionQualifier(loaded, selected, path, backend, unavailableGuestInspector{})
	if err != nil || qualifier == nil {
		t.Fatalf("exact production qualification rejected: %v", err)
	}
	selected.StateRoot += "-other"
	if _, err := NewProductionQualifier(loaded, selected, path, backend, unavailableGuestInspector{}); err == nil {
		t.Fatal("substituted domain admitted to qualification")
	}
	selected, _ = loaded.Domain("work")
	if _, err := NewProductionQualifier(loaded, selected, path, backend, nil); err == nil {
		t.Fatal("qualification accepted no trusted guest inspector")
	}
	if _, err := NewProductionQualifier(loaded, selected, "relative/config.json", backend, unavailableGuestInspector{}); err == nil {
		t.Fatal("qualification accepted noncanonical child configuration path")
	}
}
