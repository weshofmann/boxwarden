package app

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/weshofmann/boxwarden/internal/backend"
	"github.com/weshofmann/boxwarden/internal/config"
	"github.com/weshofmann/boxwarden/internal/golden"
	"github.com/weshofmann/boxwarden/internal/projectx"
	"github.com/weshofmann/boxwarden/internal/session"
	"github.com/weshofmann/boxwarden/internal/workspacex"
)

func rebuildProject(ctx context.Context, c parsedCommand, loaded config.Config, d config.Domain, r projectx.Record, o Options) error {
	if o.AlphaRebuildCandidate == nil || o.AlphaRebuildPrepare == nil {
		return errors.New("admitted project rebuild driver is required")
	}
	intent, pendingErr := projectx.LoadReplacement(d.StateRoot, d.ID, r.Name)
	if pendingErr != nil && !errors.Is(pendingErr, os.ErrNotExist) {
		return pendingErr
	}
	if !c.project.retry && pendingErr == nil {
		return fmt.Errorf("project replacement is pending; use project rebuild retry %s", r.Name)
	}
	if c.project.retry && errors.Is(pendingErr, os.ErrNotExist) {
		// A later preparation can exist while the bookmark still matches an
		// older completed receipt. Its live journal takes precedence.
		_, journalErr := session.LoadRebuildJournal(d.StateRoot, d.ID, r.Name)
		if errors.Is(journalErr, os.ErrNotExist) {
			completed, err := projectx.LoadCompletedReplacement(d.StateRoot, d.ID, r.Name)
			if err == nil {
				intent = completed
				pendingErr = nil
			} else if !errors.Is(err, os.ErrNotExist) {
				return err
			}
		} else if journalErr != nil {
			return journalErr
		}
	}
	if pendingErr != nil {
		var base string
		if c.project.retry {
			j, err := session.LoadRebuildJournal(d.StateRoot, d.ID, r.Name)
			if err != nil {
				return fmt.Errorf("no recoverable project candidate; inspect project status %s: %w", r.Name, err)
			}
			if j.SessionID != r.SessionID || j.OldBackend != r.BackendObject || j.OldRevision != r.Base || (j.Phase != session.RebuildReserved && j.Phase != session.RebuildCloned) {
				return errors.New("pending candidate differs from project binding; inspect session status before changing anything")
			}
			base = j.CandidateRevision
		} else {
			if _, err := boundProject(ctx, d, r); err != nil {
				return err
			}
			s, err := boundProjectSession(d, r)
			if err != nil {
				return err
			}
			if s.IntendedState != session.StateStopped || s.StartGeneration != "" {
				return fmt.Errorf("project rebuild requires stopped system; use project stop %s", r.Name)
			}
			if r.ImportID != "" && !r.Imported {
				return fmt.Errorf("project has a pending import; finish project import retry %s before replacing the system", r.Name)
			}
			base = c.project.base
			if base == "current" {
				g, err := golden.LoadCurrent(ctx, d)
				if err != nil {
					return err
				}
				base = g.Revision
			}
		}
		if o.BackendFactory != nil {
			deps, err := o.BackendFactory(loaded, d)
			if err != nil {
				return err
			}
			o.Observer = deps.Observer
		}
		if o.Observer == nil {
			return errors.New("admitted project observer is required")
		}
		observed, err := o.Observer.Observe(ctx, r.BackendObject)
		if err != nil || observed.ObjectID != r.BackendObject || !observed.Exists || observed.State != backend.ObjectStopped {
			return fmt.Errorf("project rebuild requires exact stopped system; use project stop %s: %v", r.Name, err)
		}
		// Preparing owns all existing stopped-system and workspace-use admission.
		// Its session journal survives even if the bookmark receipt write fails.
		j, err := o.AlphaRebuildPrepare(ctx, loaded, d, c.configPath, r.Name, base)
		if err != nil {
			return fmt.Errorf("project candidate preparation incomplete; use project rebuild retry %s after inspecting session status: %w", r.Name, err)
		}
		intent, err = projectx.BeginReplacement(d.StateRoot, d.ID, r, j)
		if err != nil {
			return fmt.Errorf("retain project candidate receipt; use project rebuild retry %s: %w", r.Name, err)
		}
	}
	if r != intent.Before && r != intent.Next() {
		return errors.New("project changed during pending system replacement")
	}
	if _, err := projectReplacementWorkspace(d, intent); err != nil {
		return err
	}
	rebuilt, err := o.AlphaRebuildCandidate(ctx, loaded, d, c.configPath, intent.Witness())
	if err != nil {
		return fmt.Errorf("project replacement incomplete; use project rebuild retry %s; workspace and import selection retained: %w", r.Name, err)
	}
	actual, err := session.LoadRecord(d.StateRoot, string(d.ID), r.Name)
	if err != nil || actual != rebuilt || actual.ID != intent.Before.SessionID || actual.Name != session.Name(r.Name) || actual.Domain != d.ID || actual.Backend.Kind != "tart" || actual.Backend.ObjectID != intent.BackendObject || actual.GoldenRevision != intent.Base || actual.Mode != session.ModeClean || actual.RecipeIntentDigest != "" {
		return fmt.Errorf("completed rebuild differs from exact project candidate: %v", err)
	}
	if _, err := boundProject(ctx, d, intent.Next()); err != nil {
		return err
	}
	next, err := projectx.CompleteReplacement(d.StateRoot, d.ID, intent)
	if err != nil {
		return fmt.Errorf("replacement completed but bookmark publication is uncertain; use project rebuild retry %s or inspect project status before another rebuild: %w", r.Name, err)
	}
	if _, err := fmt.Fprintf(o.Output, "project system replaced: %s\nold system: %s\nnew system: %s\nworkspace retained; no reformat or reimport; workspace contents are not sanitized\n", r.Name, intent.Before.BackendObject, next.BackendObject); err != nil {
		return err
	}
	return writeProject(o.Output, next)
}

func projectReplacementWorkspace(d config.Domain, intent projectx.Replacement) (workspacex.Record, error) {
	v, err := workspacex.LoadRecord(d.StateRoot, d.ID, intent.Before.VolumeID)
	if err != nil {
		return v, err
	}
	r := intent.Before
	if v.FilesystemUUID != r.FilesystemUUID || v.SizeBytes != r.SizeBytes || v.Disk == nil || v.Attachment == nil || v.Attachment.SessionID != r.SessionID || v.Attachment.SessionName != r.Name || v.Attachment.MountPath != projectMount {
		return v, errors.New("replacement workspace differs from retained project binding")
	}
	return v, nil
}
