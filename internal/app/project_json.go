package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/weshofmann/boxwarden/internal/backend"
	"github.com/weshofmann/boxwarden/internal/config"
	"github.com/weshofmann/boxwarden/internal/importx"
	"github.com/weshofmann/boxwarden/internal/projectx"
	"github.com/weshofmann/boxwarden/internal/session"
	"github.com/weshofmann/boxwarden/internal/workspacex"
)

// JSON is a presentation boundary only. Nested lifecycle commands retain their
// ordinary admission and report progress through this writer; terminal data is
// populated from typed records and admitted driver receipts, never that prose.
type projectJSON struct {
	output    io.Writer
	operation string
	data      any
	record    *projectx.Record
	mu        sync.Mutex
	written   int
	outputErr error
}

// ProjectOutput wraps stdout before CLI dependency construction so callbacks
// capturing the output writer share the same JSON-only progress boundary.
func ProjectOutput(args []string, output io.Writer) io.Writer {
	if operation, enabled := projectJSONRequest(args); enabled {
		return &projectJSON{output: output, operation: operation}
	}
	return output
}

type projectEvent struct {
	Version   int    `json:"version"`
	Type      string `json:"type"`
	Operation string `json:"operation"`
	Message   string `json:"message,omitempty"`
	Data      any    `json:"data,omitempty"`
}

const projectJSONLimit = 8 << 20
const projectJSONErrorReserve = 64 << 10

var errProjectJSONLimit = errors.New("structured project output exceeds the 8 MiB limit; inspect state before retrying")

func (j *projectJSON) emit(kind, message string, data any) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if kind != "error" && j.outputErr != nil {
		return j.outputErr
	}
	if kind == "error" && len(message) > 4096 {
		message = message[:4096]
	}
	raw, err := json.Marshal(projectEvent{1, kind, j.operation, message, data})
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	limit := projectJSONLimit - projectJSONErrorReserve
	if kind == "error" {
		limit = projectJSONLimit
	}
	if len(raw) > limit-j.written {
		j.outputErr = errProjectJSONLimit
		return errProjectJSONLimit
	}
	n, err := j.output.Write(raw)
	j.written += n
	if err == nil && n != len(raw) {
		err = io.ErrShortWrite
	}
	if err != nil {
		j.outputErr = err
	}
	return err
}
func (j *projectJSON) Write(p []byte) (int, error) {
	original := len(p)
	// Each write is bounded independently; progress content has no machine meaning.
	for len(p) > 0 {
		n := len(p)
		if n > 4096 {
			n = 4096
		}
		if err := j.emit("progress", string(p[:n]), nil); err != nil {
			return 0, err
		}
		p = p[n:]
	}
	return original, nil
}
func projectJSONRequest(args []string) (string, bool) {
	for i, a := range args {
		if a != "project" || i+1 >= len(args) {
			continue
		}
		operation := "project." + args[i+1]
		if i+2 < len(args) && (args[i+2] == "retry" || args[i+2] == "preview" || args[i+2] == "list") {
			operation += "." + args[i+2]
		}
		enabled := false
		for _, flag := range args[i+2:] {
			switch flag {
			case "--json", "--json=true":
				enabled = true
			case "--json=false":
				enabled = false
			}
		}
		return operation, enabled
	}
	return "", false
}

type projectSetupJSON struct {
	Status   string `json:"status"`
	Guidance string `json:"guidance"`
}
type projectWorkspaceJSON struct {
	ID             string `json:"id"`
	FilesystemUUID string `json:"filesystem_uuid"`
	SizeBytes      int64  `json:"size_bytes"`
	MountPath      string `json:"mount_path"`
	Initialized    bool   `json:"initialized"`
}
type projectActionJSON struct {
	ActionID   string `json:"action_id"`
	Phase      string `json:"phase"`
	State      string `json:"state"`
	Generation string `json:"generation"`
}
type projectSoftwareJSON struct {
	IntentDigest string              `json:"intent_digest"`
	Status       string              `json:"status"`
	Actions      []projectActionJSON `json:"actions"`
}
type projectImportJSON struct {
	Status    string `json:"status"`
	ID        string `json:"id"`
	GuestPath string `json:"guest_path"`
}
type projectSnapshotJSON struct {
	Name               string               `json:"name"`
	Base               string               `json:"base"`
	SessionID          string               `json:"session_id"`
	BackendObject      string               `json:"backend_object"`
	State              string               `json:"state"`
	ObservedState      string               `json:"observed_state"`
	BackendRunning     bool                 `json:"backend_running"`
	ManagementReady    bool                 `json:"management_ready"`
	Diagnostic         string               `json:"diagnostic"`
	Workspace          projectWorkspaceJSON `json:"workspace"`
	Software           projectSoftwareJSON  `json:"software"`
	Import             projectImportJSON    `json:"import"`
	ReplacementPending bool                 `json:"replacement_pending"`
	AvailableActions   []string             `json:"available_actions"`
}

func projectSetupSnapshot(ctx context.Context, d config.Domain, o Options) projectSetupJSON {
	setup, err := projectx.LoadSetup(d.StateRoot)
	if errors.Is(err, os.ErrNotExist) {
		return projectSetupJSON{"missing", "Run this package's prepare-projects.sh to admit project assets before creating a project."}
	}
	if err != nil {
		return projectSetupJSON{"invalid", err.Error() + "; inspect project setup before creating a project."}
	}
	if o.ProjectSetupCheck == nil {
		return projectSetupJSON{"unverified", "Project assets have not been checked by this command."}
	}
	if err := o.ProjectSetupCheck(ctx, d, setup); err != nil {
		return projectSetupJSON{"invalid", err.Error()}
	}
	return projectSetupJSON{"ready", "Project assets passed admission; each operation re-admits current prerequisites."}
}
func projectSnapshot(ctx context.Context, loaded config.Config, d config.Domain, r projectx.Record, o Options) (projectSnapshotJSON, error) {
	var observed backend.Observation
	var exact session.Record
	o.projectObservation = &observed
	o.projectSessionSnapshot = &exact
	state, diagnostic, err := projectListState(ctx, loaded, d, r, o)
	if err != nil {
		return projectSnapshotJSON{}, err
	}
	s := projectSnapshotJSON{Name: r.Name, Base: r.Base, SessionID: r.SessionID, BackendObject: r.BackendObject, State: state, ManagementReady: state == "READY", Diagnostic: diagnostic, Workspace: projectWorkspaceJSON{r.VolumeID, r.FilesystemUUID, r.SizeBytes, projectMount, r.Initialized}, Software: projectSoftwareJSON{r.RecipeIntentDigest, "legacy_unverified", []projectActionJSON{}}, Import: projectImportJSON{"not_imported", r.ImportID, ""}, ReplacementPending: state == "rebuild pending", AvailableActions: []string{}}
	s.ObservedState = "unknown"
	if observed.Exists {
		s.ObservedState = string(observed.State)
		s.BackendRunning = observed.State == backend.ObjectRunning
	}
	if r.ImportID != "" {
		s.Import.Status = "pending"
		s.Import.GuestPath = projectMount + "/boxwarden-import-" + r.ImportID
	}
	if r.Imported {
		s.Import.Status = "matched"
	}
	if r.RecipeIntentDigest != "" {
		s.Software.Status = "unavailable"
		if r.SessionID != "" && !s.ReplacementPending {
			attempts, e := session.ListActionAttempts(ctx, d, r.Name)
			if e != nil {
				s.Software.Status = "unknown"
			} else {
				for _, a := range attempts {
					s.Software.Actions = append(s.Software.Actions, projectActionJSON{a.ActionID, a.ActionPhase, string(a.State), a.Generation})
				}
			}
		}
		if s.ManagementReady {
			current, pending, e := session.LoadAutomaticActionPlan(ctx, d, r.Name)
			switch {
			case current != exact:
				s.Software.Status = "unknown"
			case errors.Is(e, session.ErrAutomaticActionUnresolved):
				s.Software.Status = "blocked"
			case e != nil:
				s.Software.Status = "unknown"
			case len(pending) > 0:
				s.Software.Status = "pending"
			default:
				s.Software.Status = "complete"
			}
		}
	}
	switch {
	case s.ReplacementPending:
		s.AvailableActions = []string{"rebuild_retry", "inspect_session"}
	case r.SessionID == "" || state == "unavailable":
		s.AvailableActions = []string{"inspect_session"}
	default:
		s.AvailableActions = []string{"open", "status", "stop"}
		if state == "stopped" {
			s.AvailableActions = append(s.AvailableActions, "rebuild")
		}
		if r.Imported {
			s.AvailableActions = append(s.AvailableActions, "export")
		} else if r.ImportID != "" {
			s.AvailableActions = append(s.AvailableActions, "import_retry")
		} else {
			s.AvailableActions = append(s.AvailableActions, "import")
		}
	}
	return s, nil
}
func listProjectsJSON(ctx context.Context, loaded config.Config, d config.Domain, o Options) error {
	records, err := projectx.List(d.StateRoot, d.ID)
	if err != nil {
		return err
	}
	snapshots := []projectSnapshotJSON{}
	if len(records) > 0 && o.BackendFactory != nil {
		deps, e := o.BackendFactory(loaded, d)
		if e != nil {
			return fmt.Errorf("construct project observer: %w", e)
		}
		o.Observer = deps.Observer
	}
	for _, r := range records {
		s, e := projectSnapshot(ctx, loaded, d, r, o)
		if e != nil {
			return fmt.Errorf("project %s: %w", r.Name, e)
		}
		snapshots = append(snapshots, s)
	}
	o.projectJSON.data = struct {
		Setup    projectSetupJSON      `json:"setup"`
		Projects []projectSnapshotJSON `json:"projects"`
	}{projectSetupSnapshot(ctx, d, o), snapshots}
	return nil
}
func finishProjectJSON(ctx context.Context, c parsedCommand, loaded config.Config, d config.Domain, o Options) {
	if o.projectJSON.data != nil {
		return
	}
	if c.project.operation == "setup" || c.project.operation == "setup-update" {
		o.projectJSON.data = struct {
			Setup projectSetupJSON `json:"setup"`
		}{projectSetupSnapshot(ctx, d, o)}
		return
	}
	r, err := projectx.Load(d.StateRoot, d.ID, c.project.name)
	if o.projectJSON.record != nil {
		r = *o.projectJSON.record
		err = nil
	}
	if err != nil {
		o.projectJSON.data = map[string]any{"observation_unavailable": err.Error()}
		return
	}
	if o.BackendFactory != nil {
		deps, e := o.BackendFactory(loaded, d)
		if e == nil {
			o.Observer = deps.Observer
		} else {
			o.Observer = nil
		}
	}
	snapshot, err := projectSnapshot(ctx, loaded, d, r, o)
	if err != nil {
		// The successful effect is already admitted. A failed subsequent observation
		// is uncertainty about current state, never retroactive effect failure.
		snapshot = projectSnapshotJSON{Name: r.Name, Base: r.Base, SessionID: r.SessionID, BackendObject: r.BackendObject, State: "unavailable", ObservedState: "unknown", Diagnostic: err.Error(), Workspace: projectWorkspaceJSON{r.VolumeID, r.FilesystemUUID, r.SizeBytes, projectMount, r.Initialized}, Software: projectSoftwareJSON{r.RecipeIntentDigest, "unknown", []projectActionJSON{}}, Import: projectImportJSON{"not_imported", r.ImportID, ""}, AvailableActions: []string{"inspect_session"}}
		if r.ImportID != "" {
			snapshot.Import.Status = "pending"
			snapshot.Import.GuestPath = projectMount + "/boxwarden-import-" + r.ImportID
		}
		if r.Imported {
			snapshot.Import.Status = "matched"
		}
	}
	o.projectJSON.data = struct {
		Project projectSnapshotJSON `json:"project"`
	}{snapshot}
}
func previewProjectImportJSON(ctx context.Context, p projectCommand, j *projectJSON) error {
	selection, err := importx.ParseSelection(p.selection)
	if err != nil {
		return err
	}
	snapshot, err := importx.PreviewSource(ctx, p.source, selection)
	if err != nil {
		return err
	}
	exclusions := selection.Excludes
	if exclusions == nil {
		exclusions = []string{}
	}
	j.data = struct {
		Source         string          `json:"source"`
		Entries        []importx.Entry `json:"entries"`
		FileCount      int             `json:"file_count"`
		DirectoryCount int             `json:"directory_count"`
		TotalBytes     int64           `json:"total_bytes"`
		Digest         string          `json:"digest"`
		Exclusions     []string        `json:"exclusions"`
	}{p.source, snapshot.Entries, snapshot.FileCount, snapshot.DirectoryCount, snapshot.TotalBytes, snapshot.Digest, exclusions}
	return nil
}

type projectExportJSON struct {
	Transaction  string                 `json:"transaction"`
	Phase        workspacex.ExportPhase `json:"phase"`
	Published    string                 `json:"published"`
	ProjectFiles string                 `json:"project_files"`
}

func captureProjectExport(o Options, r projectx.Record, j workspacex.ExportJournal, published string) {
	if o.projectJSON == nil {
		return
	}
	files := ""
	if j.Phase == workspacex.ExportPublished {
		files = filepath.Join(published, "boxwarden-import-"+r.ImportID)
	}
	o.projectJSON.data = projectExportJSON{j.ID, j.Phase, published, files}
}
func listProjectExportsJSON(ctx context.Context, d config.Domain, name string, j *projectJSON) error {
	r, err := projectx.Load(d.StateRoot, d.ID, name)
	if err != nil {
		return err
	}
	journals, err := workspacex.ListExportJournals(ctx, d.StateRoot, d.ID, r.VolumeID)
	if err != nil {
		return err
	}
	type entry struct {
		Transaction       string                 `json:"transaction"`
		Phase             workspacex.ExportPhase `json:"phase"`
		DestinationParent string                 `json:"destination_parent"`
		Published         string                 `json:"published"`
		ProjectFiles      string                 `json:"project_files"`
		Matches           bool                   `json:"matches_current_bookmark"`
		RetryAvailable    bool                   `json:"retry_available"`
	}
	exports := []entry{}
	for _, journal := range journals {
		matches := projectExportMatches(journal, d, r)
		published, files := "", ""
		if journal.Phase == workspacex.ExportPublished {
			published = filepath.Join(journal.DestinationParent, strings.ReplaceAll(journal.ID, "-", ""))
			if len(journal.Selected) == 1 && journal.Selected[0] == "boxwarden-import-"+r.ImportID {
				files = filepath.Join(published, journal.Selected[0])
			}
		}
		exports = append(exports, entry{journal.ID, journal.Phase, journal.DestinationParent, published, files, matches, matches && journal.Phase != workspacex.ExportPublished})
	}
	j.data = struct {
		ProjectName string  `json:"project_name"`
		Exports     []entry `json:"exports"`
	}{name, exports}
	return nil
}

func projectProgress(o Options, message string) error {
	if o.projectJSON == nil {
		return nil
	}
	return o.projectJSON.emit("progress", message, nil)
}
