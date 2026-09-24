package workspacex

import (
	"context"
	"errors"
	"fmt"
	"reflect"

	"github.com/weshofmann/boxwarden/internal/backend"
	"github.com/weshofmann/boxwarden/internal/domain"
)

// ExportSelectedWorkspace runs one stopped-workspace transaction through an
// isolated snapshot, admitted inspector, and selected receiver. On a failure
// after snapshot creation, journal.ID remains the recovery handle. The
// destination is never overwritten; an ambiguous post-rename result includes
// the exact published path with an error.
func ExportSelectedWorkspace(ctx context.Context, stateRoot string, domainID domain.ID, volumeID, destinationParent string,
	selected []string, observer backend.Observer, sourceRoot, isoPath, goBinary string) (journal ExportJournal, published string, err error) {
	journal, err = CreateExportSnapshot(ctx, stateRoot, domainID, volumeID, destinationParent, selected, observer)
	if err != nil {
		return journal, "", err
	}
	bundle, err := BuildAdmittedExportInspectorBundle(ctx, stateRoot, domainID, journal.ID, sourceRoot, isoPath, goBinary)
	if err != nil {
		return journal, "", fmt.Errorf("export %s bundle build: %w", journal.ID, err)
	}
	defer func() { err = errors.Join(err, bundle.Remove()) }()
	captured, observed, err := CaptureAdmittedExportInspector(ctx, stateRoot, domainID, journal.ID, bundle.Path, sourceRoot)
	if err != nil {
		return journal, "", fmt.Errorf("export %s inspector capture: %w", journal.ID, err)
	}
	if !reflect.DeepEqual(observed, journal) {
		return journal, "", errors.Join(fmt.Errorf("export journal changed between bundle and capture"), captured.Remove())
	}
	published, err = PublishCapturedExport(ctx, stateRoot, domainID, journal.ID, captured)
	if err != nil {
		return journal, published, fmt.Errorf("export %s publication: %w", journal.ID, err)
	}
	verified, err := loadExportJournal(stateRoot, domainID, journal.ID)
	expected := journal
	expected.Phase = ExportPublished
	if err != nil || !reflect.DeepEqual(verified, expected) {
		return journal, published, fmt.Errorf("export %s output exists but durable published state cannot be re-read: %v", journal.ID, err)
	}
	journal = verified
	return journal, published, nil
}
