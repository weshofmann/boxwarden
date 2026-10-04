package app

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"

	"github.com/weshofmann/boxwarden/internal/backend"
	"github.com/weshofmann/boxwarden/internal/config"
	"github.com/weshofmann/boxwarden/internal/projectx"
	"github.com/weshofmann/boxwarden/internal/session"
	"github.com/weshofmann/boxwarden/internal/workspacex"
)

// Retry selects an existing journal, never allocates a new destination or
// snapshot. All copy recovery, helper ownership and publication checks remain
// in the stopped-workspace driver, including ambiguous prior publication.
func retryProjectExport(ctx context.Context, c parsedCommand, loaded config.Config, d config.Domain, r projectx.Record, o Options) error {
	if !r.Imported {
		return errors.New("project has no matched import selection to export")
	}
	initial, err := workspacex.LoadExportJournal(d.StateRoot, d.ID, c.project.transaction)
	if err != nil {
		return err
	}
	if !projectExportMatches(initial, d, r) {
		return errors.New("export transaction differs from exact current project binding and selection")
	}
	if initial.Phase == workspacex.ExportPublished {
		return fmt.Errorf("export transaction is already published; inspect destination %s; retry never overwrites returned files", initial.DestinationParent)
	}
	setup, err := projectx.LoadSetup(d.StateRoot)
	if err != nil {
		return err
	}
	if o.BackendFactory != nil {
		deps, e := o.BackendFactory(loaded, d)
		if e != nil {
			return e
		}
		o.Observer = deps.Observer
	}
	if o.Observer == nil || o.AlphaExportResume == nil {
		return errors.New("project export recovery and admitted observer are required")
	}
	s, err := boundProjectSession(d, r)
	if err != nil {
		return err
	}
	observed, err := o.Observer.Observe(ctx, r.BackendObject)
	if err != nil || s.IntendedState != session.StateStopped || !observed.Exists || observed.ObjectID != r.BackendObject || observed.State != backend.ObjectStopped {
		return fmt.Errorf("project export retry requires the exact stopped sandbox; use project stop %s first: %v", r.Name, err)
	}
	input := AlphaExportResumeInput{TransactionID: initial.ID, SourceRoot: setup.SourceRoot, ISOPath: setup.ISOPath, GoBinary: setup.GoBinary}
	if err := validAlphaExportResumeInput(input); err != nil {
		return err
	}
	if err := projectProgress(o, "Recovering the retained export transaction"); err != nil {
		return err
	}
	journal, published, err := o.AlphaExportResume(ctx, d, input, o.Observer)
	if err != nil {
		return projectExportFailure(r, initial.ID, err)
	}
	expected := initial
	expected.Phase = journal.Phase
	aborted := (initial.Phase == workspacex.ExportCopying || initial.Phase == workspacex.ExportAborted) && journal.Phase == workspacex.ExportAborted
	completed := (initial.Phase == workspacex.ExportSnapshotReady || initial.Phase == workspacex.ExportInspected) && journal.Phase == workspacex.ExportPublished
	if !reflect.DeepEqual(expected, journal) || (!aborted && !completed) {
		return errors.New("project export recovery receipt differs from retained transaction")
	}
	captureProjectExport(o, r, journal, published)
	if err := writeAlphaExport(o.Output, d, journal, published); err != nil {
		return err
	}
	if aborted {
		_, err = fmt.Fprintf(o.Output, "partial copy aborted; no files published; start project export with a new destination for %s\n", r.Name)
		return err
	}
	_, err = fmt.Fprintf(o.Output, "project files: %s\n", filepath.Join(published, "boxwarden-import-"+r.ImportID))
	return err
}

func projectExportMatches(j workspacex.ExportJournal, d config.Domain, r projectx.Record) bool {
	return alphaCreateUUID(j.ID) && j.Domain == d.ID && j.VolumeID == r.VolumeID && j.SessionID == r.SessionID && j.SessionName == r.Name && j.BackendObject == r.BackendObject && j.FilesystemUUID == r.FilesystemUUID && j.SizeBytes == r.SizeBytes && len(j.Selected) == 1 && j.Selected[0] == "boxwarden-import-"+r.ImportID
}

func projectExportFailure(r projectx.Record, id string, err error) error {
	if alphaCreateUUID(id) {
		return fmt.Errorf("project export transaction %s incomplete; retained evidence; use project export retry --transaction %s %s (keep the sandbox stopped; uncertain publication or helper lifetime still requires inspection): %w", id, id, r.Name, err)
	}
	return fmt.Errorf("project export failed before a recoverable transaction was returned; inspect the retained destination and use a new destination for retry: %w", err)
}
