package workspacex

import (
	"context"
	"errors"
	"fmt"
	"os"
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
	return completeSelectedWorkspace(ctx, stateRoot, domainID, journal, sourceRoot, isoPath, goBinary)
}

type exportContinuation func(context.Context, string, domain.ID, ExportJournal, string, string, string) (ExportJournal, string, error)

// ResumeSelectedWorkspace reuses only an exact durable snapshot-ready or
// inspected transaction. An existing destination is ambiguous after a
// possible receiver rename and is never overwritten or marked published.
func ResumeSelectedWorkspace(ctx context.Context, stateRoot string, domainID domain.ID, transactionID, sourceRoot, isoPath, goBinary string) (ExportJournal, string, error) {
	return resumeSelectedWorkspace(ctx, stateRoot, domainID, transactionID, sourceRoot, isoPath, goBinary, completeSelectedWorkspace)
}

func resumeSelectedWorkspace(ctx context.Context, stateRoot string, domainID domain.ID, transactionID, sourceRoot, isoPath, goBinary string, finish exportContinuation) (ExportJournal, string, error) {
	if finish == nil {
		return ExportJournal{}, "", fmt.Errorf("export continuation is required")
	}
	journal, err := loadExportJournal(stateRoot, domainID, transactionID)
	if err != nil {
		return ExportJournal{}, "", err
	}
	if journal.Phase != ExportSnapshotReady && journal.Phase != ExportInspected {
		return journal, "", fmt.Errorf("export transaction is not ready for snapshot-based resume")
	}
	if err := admitExactExportSnapshot(ctx, stateRoot, journal); err != nil {
		return journal, "", fmt.Errorf("re-admit exact snapshot before resume: %w", err)
	}
	parent, identity, err := admitExportParent(journal.DestinationParent)
	if err != nil {
		return journal, "", fmt.Errorf("export destination is not the original empty private parent; inspect possible prior publication before retry: %w", err)
	}
	if closeErr := parent.Close(); closeErr != nil {
		return journal, "", closeErr
	}
	if identity != journal.Destination {
		return journal, "", fmt.Errorf("export destination parent identity changed before resume")
	}
	if err := requireAbsentInspectorSpool(stateRoot, journal.ID); err != nil {
		return journal, "", err
	}
	return finish(ctx, stateRoot, domainID, journal, sourceRoot, isoPath, goBinary)
}

func requireAbsentInspectorSpool(stateRoot, transactionID string) error {
	root, err := openStateRoot(stateRoot)
	if err != nil {
		return err
	}
	defer root.Close()
	exports, err := openChild(root, "exports", false)
	if err != nil {
		return err
	}
	defer exports.Close()
	transaction, err := openChild(exports, transactionID, false)
	if err != nil {
		return err
	}
	defer transaction.Close()
	if _, err := transaction.Lstat("stream.bin"); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	return fmt.Errorf("unverified private inspector spool exists; inspect the exact helper and transaction before retry")
}

func completeSelectedWorkspace(ctx context.Context, stateRoot string, domainID domain.ID, journal ExportJournal, sourceRoot, isoPath, goBinary string) (ExportJournal, string, error) {
	bundle, err := BuildAdmittedExportInspectorBundle(ctx, stateRoot, domainID, journal.ID, sourceRoot, isoPath, goBinary)
	if err != nil {
		return journal, "", fmt.Errorf("export %s bundle build: %w", journal.ID, err)
	}
	return completeSelectedWorkspaceWithBundle(ctx, stateRoot, domainID, journal, bundle, sourceRoot)
}

func completeSelectedWorkspaceWithBundle(ctx context.Context, stateRoot string, domainID domain.ID, journal ExportJournal, bundle PreparedInspectorBundle, sourceRoot string) (result ExportJournal, published string, err error) {
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
