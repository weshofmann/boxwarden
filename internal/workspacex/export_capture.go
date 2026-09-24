package workspacex

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"

	"github.com/weshofmann/boxwarden/internal/domain"
	"github.com/weshofmann/boxwarden/internal/exportx"
	"github.com/weshofmann/boxwarden/internal/lock"
)

type exportBundleAdmitter func(context.Context, string, string, []byte) (exportx.InspectorBundle, error)
type exportInspectorCapturer func(context.Context, string, []string, string) (exportx.CapturedInspectorStream, error)

// CaptureAdmittedExportInspector binds a prepared production bundle to the
// snapshot-ready journal and captures only an export-mode stopped VM stream.
// The caller owns the returned spool. Before publication it must re-admit the
// journal and snapshot under the transaction lock and use the journal's exact
// selection and destination; capture alone never authorizes publication.
func CaptureAdmittedExportInspector(ctx context.Context, stateRoot string, domainID domain.ID, transactionID, bundlePath, sourceRoot string) (exportx.CapturedInspectorStream, ExportJournal, error) {
	return captureBoundExportInspector(ctx, stateRoot, domainID, transactionID, bundlePath, sourceRoot,
		exportx.AdmitInspectorBundle, exportx.CaptureExportInspector)
}

// captureBoundExportInspector keeps the exact transaction lock through
// request preparation, artifact admission, helper reap, and snapshot rehash.
// It does not advance the journal or publish the stream. The later publication
// operation must take the transaction lock and recheck this journal and the
// snapshot again before consuming a returned spool.
func captureBoundExportInspector(ctx context.Context, stateRoot string, domainID domain.ID, transactionID, bundlePath, sourceRoot string,
	admit exportBundleAdmitter, capture exportInspectorCapturer) (result exportx.CapturedInspectorStream, journal ExportJournal, err error) {
	if _, parseErr := domain.Parse(string(domainID)); parseErr != nil || !validUUID(transactionID) || admit == nil || capture == nil {
		return result, journal, fmt.Errorf("invalid export capture binding: %v", parseErr)
	}
	held, err := lock.Acquire(ctx, stateRoot, "export-"+string(domainID)+"-"+transactionID)
	if err != nil {
		return result, journal, err
	}
	defer func() {
		if releaseErr := held.Release(); releaseErr != nil {
			if result.Stream != nil {
				releaseErr = errors.Join(releaseErr, result.Remove())
				result = exportx.CapturedInspectorStream{}
			}
			err = errors.Join(err, releaseErr)
		}
	}()
	prepared, err := prepareExportInspectorRequestLocked(ctx, stateRoot, domainID, transactionID)
	if err != nil {
		return result, journal, err
	}
	journal = prepared.Journal
	artifacts, err := admit(ctx, bundlePath, sourceRoot, prepared.Request)
	if err != nil {
		return result, journal, fmt.Errorf("admit export inspector bundle: %w", err)
	}
	if artifacts.Helper == "" || artifacts.Kernel == "" || artifacts.Initrd == "" {
		return result, journal, fmt.Errorf("inspector bundle lacks launch artifacts")
	}
	args := exportInspectorBootArgs(artifacts, prepared)
	privateParent := filepath.Dir(prepared.SnapshotPath)
	result, err = capture(ctx, artifacts.Helper, args, privateParent)
	if err != nil {
		return exportx.CapturedInspectorStream{}, journal, err
	}
	if err = admitExactExportSnapshot(ctx, stateRoot, journal); err != nil {
		return exportx.CapturedInspectorStream{}, journal, errors.Join(fmt.Errorf("snapshot changed after inspector reap: %w", err), result.Remove())
	}
	current, loadErr := loadExportJournal(stateRoot, domainID, transactionID)
	if loadErr != nil || !reflect.DeepEqual(current, journal) {
		return exportx.CapturedInspectorStream{}, journal, errors.Join(fmt.Errorf("export journal changed after inspector reap: %v", loadErr), result.Remove())
	}
	return result, journal, nil
}

func exportInspectorBootArgs(bundle exportx.InspectorBundle, prepared PreparedExportInspectorRequest) []string {
	snapshot := prepared.Journal.Snapshot.Identity
	return []string{"boot-export", bundle.Kernel, bundle.Initrd, prepared.SnapshotPath,
		strings.ReplaceAll(prepared.Journal.ID, "-", ""),
		strconv.FormatUint(snapshot.Device, 10), strconv.FormatUint(snapshot.Inode, 10),
		strconv.FormatInt(prepared.Journal.SizeBytes, 10)}
}
