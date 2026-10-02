package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/weshofmann/boxwarden/internal/config"
	"github.com/weshofmann/boxwarden/internal/lifecycle"
	"github.com/weshofmann/boxwarden/internal/projectx"
	"github.com/weshofmann/boxwarden/internal/session"
	"github.com/weshofmann/boxwarden/internal/workspacex"
)

// Listing is an unlocked read-only snapshot, not lifecycle authority. Build
// the whole result before output so corrupt or foreign bindings fail closed
// without presenting an earlier project as a trustworthy partial registry.
func listProjects(ctx context.Context, loaded config.Config, d config.Domain, o Options) error {
	records, err := projectx.List(d.StateRoot, d.ID)
	if err != nil {
		return err
	}
	if len(records) == 0 {
		_, err = fmt.Fprintln(o.Output, "no named projects; next: project create NAME")
		return err
	}
	if o.BackendFactory != nil {
		deps, e := o.BackendFactory(loaded, d)
		if e != nil {
			return fmt.Errorf("construct project observer: %w", e)
		}
		o.Observer = deps.Observer
	}
	var out bytes.Buffer
	fmt.Fprintln(&out, "projects: read-only snapshot; commands re-admit current bindings and readiness")
	for _, r := range records {
		if err := ctx.Err(); err != nil {
			return err
		}
		state, diagnostic, err := projectListState(ctx, loaded, d, r, o)
		if err != nil {
			return fmt.Errorf("project %s: %w", r.Name, err)
		}
		fmt.Fprintf(&out, "project: %s\nstate: %s\n", r.Name, state)
		if r.SessionID == "" {
			fmt.Fprintln(&out, "system: not allocated")
		} else {
			fmt.Fprintf(&out, "system: %s (session %s)\n", r.BackendObject, r.SessionID)
		}
		if diagnostic != "" {
			fmt.Fprintf(&out, "detail: %s\n", diagnostic)
		}
		fmt.Fprintf(&out, "workspace: %s (%d MiB; filesystem %s)\nmount: %s\n", r.VolumeID, r.SizeBytes>>20, r.FilesystemUUID, projectMount)
		if r.ImportID != "" {
			fmt.Fprintf(&out, "project files: %s/boxwarden-import-%s\n", projectMount, r.ImportID)
		}
		switch {
		case r.SessionID == "" || state == "unavailable":
			fmt.Fprintf(&out, "next: session status %s (inspect before changing the project)\n", r.Name)
		default:
			fmt.Fprintf(&out, "next: project open %s | project status %s | project stop %s\n", r.Name, r.Name, r.Name)
			if r.Imported {
				fmt.Fprintf(&out, "next transfer: project export --destination NEW-DIRECTORY %s (after stop)\n", r.Name)
			} else if r.ImportID != "" {
				fmt.Fprintf(&out, "next transfer: project import retry %s (same selection; when READY)\n", r.Name)
			} else {
				fmt.Fprintf(&out, "next transfer: project import --source PRIVATE-DIRECTORY %s (when READY)\n", r.Name)
			}
		}
		fmt.Fprintln(&out)
	}
	_, err = o.Output.Write(out.Bytes())
	return err
}

func projectListState(ctx context.Context, loaded config.Config, d config.Domain, r projectx.Record, o Options) (string, string, error) {
	// Even allocation-only bookmarks cannot borrow another project's volume.
	if r.SessionID == "" {
		v, err := workspacex.LoadRecord(d.StateRoot, d.ID, r.VolumeID)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return "", "", err
		}
		if err == nil && (v.FilesystemUUID != r.FilesystemUUID || v.SizeBytes != r.SizeBytes || v.Attachment != nil) {
			return "", "", errors.New("incomplete project workspace binding differs from remembered allocation")
		}
		return "incomplete", "session allocation has no exact receipt; inspect retained intent", nil
	}
	s, err := session.LoadRecord(d.StateRoot, string(d.ID), r.Name)
	if errors.Is(err, os.ErrNotExist) {
		return "unavailable", "remembered system record is missing; inspect session status", nil
	}
	if err != nil {
		return "", "", err
	}
	if s.ID != r.SessionID || s.Backend.Kind != "tart" || s.Backend.ObjectID != r.BackendObject || s.GoldenRevision != r.Base || s.Mode != session.ModeClean {
		return "", "", errors.New("project session binding differs from remembered identity")
	}
	if _, err := session.LoadRebuildJournal(d.StateRoot, d.ID, r.Name); err == nil {
		return "unavailable", "system rebuild is pending; inspect session status before further operations", nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", "", err
	}
	v, err := workspacex.LoadRecord(d.StateRoot, d.ID, r.VolumeID)
	if errors.Is(err, os.ErrNotExist) && !r.Initialized {
		return "incomplete", "workspace initialization is incomplete; use project open", nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return "unavailable", "remembered workspace record is missing", nil
	}
	if err != nil {
		return "", "", err
	}
	if v.FilesystemUUID != r.FilesystemUUID || v.SizeBytes != r.SizeBytes || v.Attachment != nil && (v.Attachment.SessionID != r.SessionID || v.Attachment.SessionName != r.Name || v.Attachment.MountPath != projectMount) {
		return "", "", errors.New("project workspace binding differs from remembered identity")
	}
	if !r.Initialized {
		return "incomplete", "workspace initialization has no receipt; use project open", nil
	}
	if v.Attachment == nil || v.Disk == nil {
		return "", "", errors.New("initialized project lacks exact workspace binding")
	}
	if v.State != workspacex.StateAvailable || v.Pending != nil {
		return "unavailable", "workspace is incomplete or has a pending operation; inspect workspace before reopening", nil
	}
	disk, err := workspacex.InspectManagedDisk(d.StateRoot, r.VolumeID, r.SizeBytes)
	if errors.Is(err, os.ErrNotExist) {
		return "unavailable", "remembered workspace disk is missing", nil
	}
	if err != nil {
		return "", "", err
	}
	if disk != *v.Disk {
		return "", "", errors.New("workspace disk identity differs from remembered binding")
	}
	if s.IntendedState == session.StateRunning && v.Use == nil {
		return "unavailable", "running system lacks its exact workspace use reservation; inspect session status", nil
	}
	if v.Use != nil && (v.Use.BackendObject != r.BackendObject || v.Use.BackendKind != s.Backend.Kind || v.Use.Generation != s.StartGeneration) {
		return "unavailable", "workspace use differs from the current system generation; inspect session status", nil
	}
	if o.Observer == nil {
		return "unavailable", "backend observer is unavailable", nil
	}
	observed, err := o.Observer.Observe(ctx, r.BackendObject)
	if err != nil {
		if ctx.Err() != nil {
			return "", "", ctx.Err()
		}
		return "unavailable", "backend observation failed: " + err.Error(), nil
	}
	if observed.Exists && observed.ObjectID != r.BackendObject {
		return "", "", errors.New("backend observation differs from exact project binding")
	}
	reconciled := lifecycle.Reconcile(s.IntendedState, observed)
	reconciled, readiness := reconcileStatusSnapshot(ctx, loaded, d, s, observed, reconciled, o.StatusSnapshotFactory)
	if reconciled.Consistency != lifecycle.Consistent {
		return "unavailable", reconciled.Diagnostic, nil
	}
	if s.IntendedState == session.StateRunning && readiness == session.ReadinessReady {
		return "READY", "exact live supervisor generation passed fresh readiness checks", nil
	}
	return string(s.IntendedState), "", nil
}
