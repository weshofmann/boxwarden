package app

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/weshofmann/boxwarden/internal/backend"
	"github.com/weshofmann/boxwarden/internal/config"
	"github.com/weshofmann/boxwarden/internal/golden"
	"github.com/weshofmann/boxwarden/internal/lock"
	"github.com/weshofmann/boxwarden/internal/projectx"
	"github.com/weshofmann/boxwarden/internal/session"
	"github.com/weshofmann/boxwarden/internal/sshx"
	"github.com/weshofmann/boxwarden/internal/workspacex"
)

const projectMount = "/home/boxwarden/workspaces/project"
const projectUsage = "project setup --source-root PATH --formatter-bundle PATH --iso PATH --go PATH; project create [--base current|REGISTERED-BASE] [--size-mib 16..1024] NAME; project open|status|stop NAME; project import --source PRIVATE-DIRECTORY NAME; project import retry NAME; project export --destination NEW-DIRECTORY NAME"

type projectCommand struct {
	operation, name, base, source, destination string
	sizeMiB                                    int64
	setup                                      projectx.Setup
	retry                                      bool
}

func parseProject(args []string) (projectCommand, error) {
	var p projectCommand
	if len(args) == 0 {
		return p, errors.New(projectUsage)
	}
	p.operation = args[0]
	if p.operation == "import" && len(args) > 1 && args[1] == "retry" {
		p.retry = true
		args = append([]string{args[0]}, args[2:]...)
	}
	set := flag.NewFlagSet("project "+p.operation, flag.ContinueOnError)
	set.SetOutput(io.Discard)
	switch p.operation {
	case "setup":
		p.setup.Version = 1
		set.StringVar(&p.setup.SourceRoot, "source-root", "", "clean committed source checkout")
		set.StringVar(&p.setup.FormatterBundle, "formatter-bundle", "", "admitted private formatter bundle")
		set.StringVar(&p.setup.ISOPath, "iso", "", "admitted Ubuntu Desktop ARM64 ISO")
		set.StringVar(&p.setup.GoBinary, "go", "", "absolute Go executable")
	case "create":
		set.StringVar(&p.base, "base", "current", "registered prepared base or current")
		set.Int64Var(&p.sizeMiB, "size-mib", 64, "workspace size in MiB, 16..1024")
	case "import":
		set.StringVar(&p.source, "source", "", "explicit private source directory")
	case "export":
		set.StringVar(&p.destination, "destination", "", "new host destination")
	case "open", "status", "stop":
	default:
		return p, errors.New(projectUsage)
	}
	if err := set.Parse(args[1:]); err != nil {
		return p, err
	}
	if p.operation == "setup" {
		if len(set.Args()) != 0 {
			return p, errors.New(projectUsage)
		}
		for _, path := range []string{p.setup.SourceRoot, p.setup.FormatterBundle, p.setup.ISOPath, p.setup.GoBinary} {
			if !cleanProjectPath(path) {
				return p, errors.New("project setup requires four clean absolute asset paths")
			}
		}
		if filepath.Base(p.setup.GoBinary) != "go" {
			return p, errors.New("project setup requires an exact Go executable")
		}
		return p, nil
	}
	if len(set.Args()) != 1 {
		return p, errors.New(projectUsage)
	}
	p.name = set.Args()[0]
	if _, err := session.ParseName(p.name); err != nil {
		return p, err
	}
	if p.operation == "create" {
		if p.sizeMiB < 16 || p.sizeMiB > 1024 {
			return p, errors.New("project workspace size must be 16..1024 MiB for supported export")
		}
		if p.base != "current" {
			if err := backend.ValidateObjectID(p.base); err != nil {
				return p, err
			}
		}
	}
	if p.operation == "import" && p.retry && p.source != "" {
		return p, errors.New("project import retry reuses the original selection; do not supply --source")
	}
	if p.operation == "import" && !p.retry && !cleanProjectPath(p.source) {
		return p, errors.New("project import requires a clean absolute --source directory")
	}
	if p.operation == "export" && !cleanProjectPath(p.destination) {
		return p, errors.New("project export requires a clean absolute --destination directory")
	}
	return p, nil
}

func cleanProjectPath(path string) bool {
	return filepath.IsAbs(path) && filepath.Clean(path) == path && !strings.ContainsAny(path, "\r\n\x00")
}

// Project bookmarks contain intent and locators, never readiness or storage
// authority. Every effect goes through the existing admitted operation.
func runProject(ctx context.Context, c parsedCommand, loaded config.Config, d config.Domain, o Options) (err error) {
	if d.ID != "alpha" {
		return errors.New("project workflow currently supports the explicit alpha domain")
	}
	p := c.project
	scope := "project-" + string(d.ID) + "-" + p.name
	if p.operation == "setup" {
		scope = "project-setup-" + string(d.ID)
	}
	held, err := lock.Acquire(ctx, d.StateRoot, scope)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, held.Release()) }()
	if p.operation == "setup" {
		if o.ProjectSetupCheck == nil {
			return errors.New("project setup asset checker is required")
		}
		if err := o.ProjectSetupCheck(ctx, d, p.setup); err != nil {
			return fmt.Errorf("project setup prerequisites: %w", err)
		}
		if err := projectx.SaveSetup(d.StateRoot, p.setup); err != nil {
			return err
		}
		_, err = fmt.Fprintln(o.Output, "project setup: saved; ready for project create")
		return err
	}
	if p.operation == "create" {
		return createProject(ctx, c, loaded, d, o)
	}
	r, err := projectx.Load(d.StateRoot, d.ID, p.name)
	if err != nil {
		return fmt.Errorf("load project %s: %w", p.name, err)
	}
	if r.SessionID == "" {
		return fmt.Errorf("project %s has an incomplete session allocation; inspect session status %s; no existing session will be adopted", r.Name, r.Name)
	}
	if p.operation == "open" && !r.Initialized {
		if err := initializeProject(ctx, c, d, &r, o); err != nil {
			return err
		}
	}
	if _, err := boundProject(ctx, d, r); err != nil {
		return err
	}
	switch p.operation {
	case "open":
		if err := projectSessionCommand(ctx, c, "start", o); err != nil {
			return err
		}
		return projectStatus(ctx, c, d, r, o)
	case "status":
		return projectStatus(ctx, c, d, r, o)
	case "stop":
		if err := projectSessionCommand(ctx, c, "stop", o); err != nil {
			return err
		}
		return writeProject(o.Output, r)
	case "import":
		return importProject(ctx, c, d, r, o)
	case "export":
		return exportProject(ctx, c, loaded, d, r, o)
	}
	return errors.New(projectUsage)
}

func projectSessionCommand(ctx context.Context, c parsedCommand, op string, o Options) error {
	return Run(ctx, []string{"--config", c.configPath, "--domain", c.domain, "session", op, c.project.name}, o)
}

func createProject(ctx context.Context, c parsedCommand, loaded config.Config, d config.Domain, o Options) error {
	p := c.project
	if _, err := projectx.Load(d.StateRoot, d.ID, p.name); err == nil {
		return fmt.Errorf("project %s already exists; use project open %s", p.name, p.name)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if _, err := session.LoadRecord(d.StateRoot, string(d.ID), p.name); err == nil {
		return fmt.Errorf("session name %s already exists; choose another project name", p.name)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	setup, err := projectx.LoadSetup(d.StateRoot)
	if err != nil {
		return fmt.Errorf("project setup is missing or invalid; run project setup first: %w", err)
	}
	if o.ProjectSetupCheck == nil {
		return errors.New("project setup asset checker is required")
	}
	if err := o.ProjectSetupCheck(ctx, d, setup); err != nil {
		return fmt.Errorf("project prerequisites: %w", err)
	}
	if o.BackendFactory != nil {
		dependencies, err := o.BackendFactory(loaded, d)
		if err != nil {
			return err
		}
		o.Observer, o.Creator = dependencies.Observer, dependencies.Creator
	}
	if o.Observer == nil || o.Creator == nil {
		return errors.New("project requires admitted backend observer and creator")
	}
	base := p.base
	if base == "current" {
		g, err := golden.LoadCurrent(ctx, d)
		if err != nil {
			return fmt.Errorf("selected prepared base unavailable: %w", err)
		}
		base = g.Revision
	}
	baseLock, err := golden.AcquireLock(ctx, d)
	if err != nil {
		return err
	}
	_, err = golden.LoadRevisionLocked(d, base)
	err = errors.Join(err, baseLock.Release())
	if err != nil {
		return fmt.Errorf("selected prepared base unavailable: %w", err)
	}
	volume, err := sshx.RandomUUID()
	if err != nil {
		return err
	}
	fs, err := sshx.RandomUUID()
	if err != nil {
		return err
	}
	r := projectx.Record{Version: 1, Domain: d.ID, Name: p.name, Base: base, VolumeID: volume, FilesystemUUID: fs, SizeBytes: p.sizeMiB << 20}
	if err := projectx.Create(d.StateRoot, d.ID, r); err != nil {
		return err
	}
	fresh, err := session.NewService(d, o.Observer, o.Creator).CreateFreshFromRevision(ctx, p.name, session.ModeClean, base)
	if err != nil {
		return fmt.Errorf("project session creation incomplete; retained project intent: %w", err)
	}
	r.SessionID, r.BackendObject = fresh.Record.ID, fresh.Record.Backend.ObjectID
	if err := projectx.Save(d.StateRoot, d.ID, r); err != nil {
		return fmt.Errorf("persist exact project session receipt: %w", err)
	}
	if err := initializeProject(ctx, c, d, &r, o); err != nil {
		return fmt.Errorf("project %s retained; retry project open %s: %w", p.name, p.name, err)
	}
	if err := projectSessionCommand(ctx, c, "start", o); err != nil {
		return err
	}
	return projectStatus(ctx, c, d, r, o)
}

func initializeProject(ctx context.Context, c parsedCommand, d config.Domain, r *projectx.Record, o Options) error {
	if _, err := boundProjectSession(d, *r); err != nil {
		return err
	}
	volume, err := workspacex.LoadRecord(d.StateRoot, d.ID, r.VolumeID)
	if errors.Is(err, os.ErrNotExist) || err == nil && volume.Disk == nil {
		setup, loadErr := projectx.LoadSetup(d.StateRoot)
		if loadErr != nil {
			return loadErr
		}
		if o.AlphaWorkspaceCreate == nil {
			return errors.New("managed workspace formatter is required")
		}
		_, err = o.AlphaWorkspaceCreate(ctx, d, c.configPath, AlphaWorkspaceCreateInput{VolumeID: r.VolumeID, FilesystemUUID: r.FilesystemUUID, SizeBytes: r.SizeBytes, BundlePath: setup.FormatterBundle, SourceRoot: setup.SourceRoot})
		if err != nil {
			return err
		}
		volume, err = workspacex.LoadRecord(d.StateRoot, d.ID, r.VolumeID)
	}
	if err != nil {
		return err
	}
	if volume.FilesystemUUID != r.FilesystemUUID || volume.SizeBytes != r.SizeBytes {
		return errors.New("project workspace allocation differs from remembered intent")
	}
	if volume.Attachment == nil {
		if err := Run(ctx, []string{"--config", c.configPath, "--domain", c.domain, "workspace", "attach", "--mount", projectMount, r.VolumeID, r.Name}, o); err != nil {
			return err
		}
	}
	if _, err := boundProject(ctx, d, *r); err != nil {
		return err
	}
	r.Initialized = true
	return projectx.Save(d.StateRoot, d.ID, *r)
}

func boundProjectSession(d config.Domain, r projectx.Record) (session.Record, error) {
	s, err := session.LoadRecord(d.StateRoot, string(d.ID), r.Name)
	if err != nil {
		return s, err
	}
	if r.SessionID == "" || s.ID != r.SessionID || s.Backend.ObjectID != r.BackendObject || s.Backend.Kind != "tart" || s.GoldenRevision != r.Base || s.Mode != session.ModeClean {
		return s, errors.New("project session identity differs from remembered binding")
	}
	if err := session.RequireNoRebuild(d.StateRoot, d.ID, r.Name); err != nil {
		return s, err
	}
	return s, nil
}

func boundProject(ctx context.Context, d config.Domain, r projectx.Record) (workspacex.Record, error) {
	if _, err := boundProjectSession(d, r); err != nil {
		return workspacex.Record{}, err
	}
	v, err := workspacex.LoadRecord(d.StateRoot, d.ID, r.VolumeID)
	if err != nil {
		return v, err
	}
	if v.FilesystemUUID != r.FilesystemUUID || v.SizeBytes != r.SizeBytes || v.Disk == nil || v.Attachment == nil || v.Attachment.SessionID != r.SessionID || v.Attachment.SessionName != r.Name || v.Attachment.MountPath != projectMount {
		return v, errors.New("project workspace identity/attachment differs from remembered binding")
	}
	return v, ctx.Err()
}

func projectStatus(ctx context.Context, c parsedCommand, d config.Domain, r projectx.Record, o Options) error {
	if err := projectSessionCommand(ctx, c, "status", o); err != nil {
		return err
	}
	return writeProject(o.Output, r)
}

func writeProject(out io.Writer, r projectx.Record) error {
	phase := "not imported"
	remote := "(none)"
	if r.ImportID != "" {
		phase = "pending; do not reimport"
		remote = projectMount + "/boxwarden-import-" + r.ImportID
	}
	if r.Imported {
		phase = "readback matched; open never reimports"
	}
	_, err := fmt.Fprintf(out, "project: %s\nworkspace: %s (%d MiB)\nmount: %s\nproject files: %s\nproject import: %s\nnext: project open %s | project status %s | project stop %s\n", r.Name, r.VolumeID, r.SizeBytes>>20, projectMount, remote, phase, r.Name, r.Name, r.Name)
	if err != nil {
		return err
	}
	if r.Imported {
		_, err = fmt.Fprintf(out, "next transfer: project export --destination NEW-DIRECTORY %s (after stop)\n", r.Name)
	} else if r.ImportID == "" {
		_, err = fmt.Fprintf(out, "next transfer: project import --source PRIVATE-DIRECTORY %s (when READY)\n", r.Name)
	} else {
		_, err = fmt.Fprintf(out, "next transfer: project import retry %s (same selection; when READY)\n", r.Name)
	}
	return err
}

func importProject(ctx context.Context, c parsedCommand, d config.Domain, r projectx.Record, o Options) error {
	if r.Imported {
		return errors.New("project import already readback matched; open never reimports over guest edits")
	}
	if r.ImportID != "" && !c.project.retry {
		return fmt.Errorf("project import is pending; use project import retry %s for the same selection", r.Name)
	}
	if r.ImportID == "" && c.project.retry {
		return errors.New("project has no pending import; use project import --source PRIVATE-DIRECTORY NAME first")
	}
	if o.AlphaImport == nil {
		return errors.New("project importer is required")
	}
	if r.ImportID == "" {
		id, err := sshx.RandomUUID()
		if err != nil {
			return err
		}
		r.ImportID, r.ImportSource = id, c.project.source
		if err := projectx.Save(d.StateRoot, d.ID, r); err != nil {
			return err
		}
	}
	input := AlphaImportInput{TransactionID: r.ImportID, SourcePath: r.ImportSource, VolumeID: r.VolumeID, SessionName: r.Name}
	if c.project.retry {
		_, journalErr := workspacex.LoadImportJournal(d.StateRoot, d.ID, r.ImportID)
		if journalErr != nil && !errors.Is(journalErr, os.ErrNotExist) {
			return journalErr
		}
		// Absence permits a fresh capture with the same ID/source. Any retained
		// snapshot or journal goes through resume admission, never replacement.
		_, snapshotErr := os.Lstat(filepath.Join(d.StateRoot, "imports", r.ImportID))
		if snapshotErr != nil && !errors.Is(snapshotErr, os.ErrNotExist) {
			return snapshotErr
		}
		if journalErr == nil || snapshotErr == nil {
			input.Resume, input.SourcePath = true, ""
		}
	}
	journal, receipt, err := o.AlphaImport(ctx, d, input)
	if err != nil {
		return fmt.Errorf("project import incomplete; retained selection, no automatic reimport; use project import retry %s: %w", r.Name, err)
	}
	if journal.SessionID != r.SessionID || journal.BackendObject != r.BackendObject || journal.FilesystemUUID != r.FilesystemUUID || journal.MountPath != projectMount {
		return errors.New("project import receipt differs from exact project binding")
	}
	if err := writeAlphaImport(io.Discard, d, input, journal, receipt); err != nil {
		return err
	}
	r.Imported = true
	if err := projectx.Save(d.StateRoot, d.ID, r); err != nil {
		return fmt.Errorf("project import receipt not saved; do not reimport: %w", err)
	}
	return writeProject(o.Output, r)
}

func exportProject(ctx context.Context, c parsedCommand, loaded config.Config, d config.Domain, r projectx.Record, o Options) error {
	if !r.Imported {
		return errors.New("project has no matched import selection to export")
	}
	setup, err := projectx.LoadSetup(d.StateRoot)
	if err != nil {
		return err
	}
	destination := c.project.destination
	if o.BackendFactory != nil {
		dependencies, err := o.BackendFactory(loaded, d)
		if err != nil {
			return err
		}
		o.Observer = dependencies.Observer
	}
	if o.Observer == nil || o.AlphaExport == nil {
		return errors.New("project exporter and admitted observer are required")
	}
	s, err := boundProjectSession(d, r)
	if err != nil {
		return err
	}
	observed, err := o.Observer.Observe(ctx, r.BackendObject)
	if err != nil || s.IntendedState != session.StateStopped || !observed.Exists || observed.ObjectID != r.BackendObject || observed.State != backend.ObjectStopped {
		return fmt.Errorf("project export requires the exact stopped sandbox; use project stop %s first: %v", r.Name, err)
	}
	// The underlying exporter re-admits the new private parent and reserves a
	// unique returned tree. Existing host destinations are never adopted.
	if err := os.Mkdir(destination, 0o700); err != nil {
		return fmt.Errorf("export requires a new destination: %w", err)
	}
	selection := "boxwarden-import-" + r.ImportID
	input := AlphaExportInput{VolumeID: r.VolumeID, DestinationParent: destination, Selected: []string{selection}, SourceRoot: setup.SourceRoot, ISOPath: setup.ISOPath, GoBinary: setup.GoBinary}
	if err := validAlphaExportInput(input); err != nil {
		return err
	}
	journal, published, err := o.AlphaExport(ctx, d, input, o.Observer)
	if err != nil {
		return fmt.Errorf("project export transaction %s incomplete; retained evidence: %w", journal.ID, err)
	}
	if journal.Phase != workspacex.ExportPublished || !alphaCreateUUID(journal.ID) || journal.Domain != d.ID || journal.VolumeID != r.VolumeID || journal.SessionID != r.SessionID || journal.SessionName != r.Name || journal.BackendObject != r.BackendObject || journal.FilesystemUUID != r.FilesystemUUID || journal.SizeBytes != r.SizeBytes || journal.DestinationParent != destination || len(journal.Selected) != 1 || journal.Selected[0] != selection {
		return errors.New("project export receipt differs from exact project binding and selection")
	}
	if err := writeAlphaExport(o.Output, d, journal, published); err != nil {
		return err
	}
	_, err = fmt.Fprintf(o.Output, "project files: %s\n", filepath.Join(published, selection))
	return err
}
