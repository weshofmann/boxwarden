// Package app composes the Boxwarden control-plane command without binding it
// to a particular VM backend implementation.
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
	"time"

	"github.com/weshofmann/boxwarden/internal/backend"
	"github.com/weshofmann/boxwarden/internal/config"
	"github.com/weshofmann/boxwarden/internal/golden"
	"github.com/weshofmann/boxwarden/internal/hostx"
	"github.com/weshofmann/boxwarden/internal/lifecycle"
	"github.com/weshofmann/boxwarden/internal/recipe"
	"github.com/weshofmann/boxwarden/internal/session"
	"github.com/weshofmann/boxwarden/internal/sshx"
	"github.com/weshofmann/boxwarden/internal/supervisor"
	"github.com/weshofmann/boxwarden/internal/workspacex"
)

type HostInitializer interface {
	Init(context.Context, hostx.Request) (hostx.InitResult, error)
}

type HostDoctor interface {
	Doctor(context.Context, hostx.Request) hostx.Report
}

type CAInitializer interface {
	Init(context.Context, sshx.Domain, []sshx.Domain) (sshx.CAInitResult, error)
}

// SessionStarter is the only public-command authority required for start.
// App does not learn its runtime, supervisor, or credential internals.
type SessionStarter interface {
	Start(context.Context, string) (session.Record, error)
}

// SessionStarterFactory receives only the admitted configuration, selected
// domain, and exact configuration locator for the detached child's reload.
type SessionStarterFactory func(config.Config, config.Domain, string) (SessionStarter, error)

type SessionStopper interface {
	Stop(context.Context, string) (session.Record, error)
}

type SessionStopperFactory func(config.Config, config.Domain, string) (SessionStopper, error)

type AlphaRebuildFunc func(context.Context, config.Config, config.Domain, string, string, string) (session.Record, error)

// StatusSnapshotReader is read-only evidence from one exact live supervisor.
// A backend process observation or persisted success cannot substitute for it.
type StatusSnapshotReader interface {
	Snapshot(context.Context, supervisor.Binding) (supervisor.Snapshot, error)
}

type StatusSnapshotFactory func(config.Config, config.Domain) (StatusSnapshotReader, error)

// BackendDependencies keeps observation and creation in one backend namespace.
type BackendDependencies struct {
	Observer backend.Observer
	Creator  backend.Creator
}

// BackendFactory receives the admitted configuration and exact selected domain.
// When provided, its result replaces both directly injected backend dependencies;
// missing factory dependencies never fall back to direct injection.
type BackendFactory func(config.Config, config.Domain) (BackendDependencies, error)

// Options supplies trusted-host dependencies to Run. App depends only on the
// narrow backend seams and never on Tart directly.
type Options struct {
	ConfigPath            string
	Env                   []string
	Observer              backend.Observer
	Creator               backend.Creator
	BackendFactory        BackendFactory
	HostInit              HostInitializer
	HostDoctor            HostDoctor
	CAInit                CAInitializer
	SessionStarter        SessionStarter
	SessionStarterFactory SessionStarterFactory
	SessionStopper        SessionStopper
	SessionStopperFactory SessionStopperFactory
	StatusSnapshotFactory StatusSnapshotFactory
	AlphaPrepare          AlphaPrepareFunc
	AlphaRebuild          AlphaRebuildFunc
	AlphaExport           AlphaExportFunc
	AlphaExportResume     AlphaExportResumeFunc
	Output                io.Writer
}

// DefaultConfigPath returns the conventional trusted-host configuration path.
func DefaultConfigPath() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve user configuration directory: %w", err)
	}
	return filepath.Join(base, "boxwarden", "config.json"), nil
}

// Run executes one Boxwarden command. Commands that own domain state require an
// explicit domain; host-global commands deliberately do not select one.
func Run(ctx context.Context, args []string, options Options) error {
	command, err := parseCommand(args, options)
	if err != nil {
		return err
	}
	if options.Output == nil {
		return errors.New("command output is required")
	}

	loadConfig := config.Load
	if command.kind == commandDomainInit {
		loadConfig = config.LoadDomains
	}
	loaded, err := loadConfig(command.configPath)
	if err != nil {
		return fmt.Errorf("load configuration: %w", err)
	}
	var selectedDomain config.Domain
	if command.requiresDomain() {
		selectedDomain, err = loaded.Domain(command.domain)
		if err != nil {
			return err
		}
	}
	if command.requiresBackend() {
		if command.kind == commandGoldenRegister {
			err = backend.ValidateObjectID(command.name)
		} else if command.kind != commandWorkspaceExport {
			_, err = session.ParseName(command.name)
		}
		if err != nil {
			return err
		}
		if options.BackendFactory != nil {
			dependencies, err := options.BackendFactory(loaded, selectedDomain)
			if err != nil {
				return fmt.Errorf("construct backend: %w", err)
			}
			options.Observer, options.Creator = dependencies.Observer, dependencies.Creator
		}
	}

	switch command.kind {
	case commandAlphaPrepare:
		if options.AlphaPrepare == nil {
			return errors.New("alpha base preparer is required")
		}
		result, err := options.AlphaPrepare(ctx, loaded, selectedDomain, command.configPath, command.alphaPrepare)
		if err != nil {
			return fmt.Errorf("prepare alpha base: %w", err)
		}
		return writeAlphaPrepared(options.Output, selectedDomain, result)
	case commandAlphaRecipeCheck:
		if _, err := recipe.Load(command.recipePath); err != nil {
			return fmt.Errorf("check alpha recipe: %w", err)
		}
		if err := recipe.VerifyISO(command.isoPath); err != nil {
			return fmt.Errorf("check alpha installer: %w", err)
		}
		_, err := fmt.Fprintf(options.Output, "domain: %s\nrecipe: valid\ninstaller: verified\n", command.domain)
		return err
	case commandInit:
		if options.HostInit == nil {
			return errors.New("host initializer is required")
		}
		hostRequest, err := hostRequest(loaded)
		if err != nil {
			return err
		}
		result, err := options.HostInit.Init(ctx, hostRequest)
		if err != nil {
			return fmt.Errorf("initialize host prerequisites: %w", err)
		}
		return writeInit(options.Output, result)
	case commandDoctor:
		if options.HostDoctor == nil {
			return errors.New("host doctor is required")
		}
		hostRequest, err := hostRequest(loaded)
		if err != nil {
			return err
		}
		report := options.HostDoctor.Doctor(ctx, hostRequest)
		report.Normalize()
		if err := writeDoctor(options.Output, report); err != nil {
			return err
		}
		if report.Status != hostx.Healthy {
			return fmt.Errorf("doctor found %s host prerequisites", report.Status)
		}
		return nil
	case commandDomainInit:
		if options.CAInit == nil {
			return errors.New("domain CA initializer is required")
		}
		selectedCADomain := sshx.Domain{ID: selectedDomain.ID, StateRoot: selectedDomain.StateRoot}
		configuredCADomains := configuredCADomains(loaded)
		result, err := options.CAInit.Init(ctx, selectedCADomain, configuredCADomains)
		if err != nil {
			return fmt.Errorf("initialize domain management CA: %w", err)
		}
		return writeDomainInit(options.Output, command.domain, result.Disposition)
	case commandSessionStatus:
		if options.Observer == nil {
			return errors.New("backend observer is required")
		}
		record, err := session.LoadRecord(selectedDomain.StateRoot, command.domain, command.name)
		if err != nil {
			return fmt.Errorf("load session record: %w", err)
		}
		observed, err := options.Observer.Observe(ctx, record.Backend.ObjectID)
		if err != nil {
			return fmt.Errorf("observe backend object %q: %w", record.Backend.ObjectID, err)
		}
		reconciled := lifecycle.Reconcile(record.IntendedState, observed)
		if observed.Exists && observed.ObjectID != record.Backend.ObjectID {
			reconciled = lifecycle.Reconciliation{Consistency: lifecycle.Drift, Diagnostic: "backend observation does not match the exact session object"}
		}
		if _, rebuildErr := session.LoadRebuildJournal(selectedDomain.StateRoot, record.Domain, string(record.Name)); rebuildErr == nil {
			reconciled = lifecycle.Reconciliation{Consistency: lifecycle.Drift, Diagnostic: "system rebuild in progress; readiness requires rebuild reconciliation"}
			return writeStatus(options.Output, record, observed, reconciled, session.ReadinessDrift)
		} else if !errors.Is(rebuildErr, os.ErrNotExist) {
			reconciled = lifecycle.Reconciliation{Consistency: lifecycle.Drift, Diagnostic: "system rebuild journal is unavailable or invalid"}
			return writeStatus(options.Output, record, observed, reconciled, session.ReadinessDrift)
		}
		reconciled, readiness := reconcileStatusSnapshot(ctx, loaded, selectedDomain, record, observed, reconciled, options.StatusSnapshotFactory)
		return writeStatus(options.Output, record, observed, reconciled, readiness)
	case commandGoldenRegister:
		if options.Observer == nil {
			return errors.New("backend observer is required")
		}
		record, err := golden.Register(ctx, selectedDomain, command.name, options.Observer)
		if err != nil {
			return fmt.Errorf("register golden: %w", err)
		}
		return writeGoldenRegistration(options.Output, record)
	case commandSessionCreate:
		if options.Observer == nil {
			return errors.New("backend observer is required")
		}
		if options.Creator == nil {
			return errors.New("backend creator is required")
		}
		creator := session.NewService(selectedDomain, options.Observer, options.Creator)
		var record session.Record
		if command.recipeCreate {
			if options.AlphaPrepare == nil {
				return errors.New("alpha base preparer is required for recipe session creation")
			}
			prepared, prepareErr := options.AlphaPrepare(ctx, loaded, selectedDomain, command.configPath, command.alphaPrepare)
			if prepareErr != nil {
				return fmt.Errorf("prepare session base: %w", prepareErr)
			}
			if err := validateAlphaPrepared(selectedDomain, prepared); err != nil {
				return err
			}
			record, err = creator.CreateFromRevision(ctx, command.name, command.mode, prepared.Record.CandidateID)
		} else {
			record, err = creator.Create(ctx, command.name, command.mode)
		}
		if err != nil {
			return fmt.Errorf("create session: %w", err)
		}
		return writeCreatedSession(options.Output, record)
	case commandSessionStart:
		if _, err := session.ParseName(command.name); err != nil {
			return err
		}
		starter := options.SessionStarter
		if options.SessionStarterFactory != nil {
			starter, err = options.SessionStarterFactory(loaded, selectedDomain, command.configPath)
			if err != nil {
				return fmt.Errorf("construct session starter: %w", err)
			}
		}
		if starter == nil {
			return errors.New("session starter is required")
		}
		record, err := starter.Start(ctx, command.name)
		if err != nil {
			return fmt.Errorf("start session: %w", err)
		}
		return writeStartedSession(options.Output, record)
	case commandSessionStop:
		if _, err := session.ParseName(command.name); err != nil {
			return err
		}
		stopper := options.SessionStopper
		if options.SessionStopperFactory != nil {
			stopper, err = options.SessionStopperFactory(loaded, selectedDomain, command.configPath)
			if err != nil {
				return fmt.Errorf("construct session stopper: %w", err)
			}
		}
		if stopper == nil {
			return errors.New("session stopper is required")
		}
		record, err := stopper.Stop(ctx, command.name)
		if err != nil {
			return fmt.Errorf("stop session: %w", err)
		}
		return writeStoppedSession(options.Output, record)
	case commandSessionRebuild:
		if _, err := session.ParseName(command.name); err != nil {
			return err
		}
		revision := command.rebuildBase
		if command.recipeCreate {
			if options.AlphaPrepare == nil {
				return errors.New("alpha base preparer is required for recipe rebuild")
			}
			prepared, prepareErr := options.AlphaPrepare(ctx, loaded, selectedDomain, command.configPath, command.alphaPrepare)
			if prepareErr != nil {
				return fmt.Errorf("prepare rebuild base: %w", prepareErr)
			}
			if err := validateAlphaPrepared(selectedDomain, prepared); err != nil {
				return err
			}
			revision = prepared.Record.CandidateID
		}
		if options.AlphaRebuild == nil {
			return errors.New("alpha rebuilder is required")
		}
		rebuilt, err := options.AlphaRebuild(ctx, loaded, selectedDomain, command.configPath, command.name, revision)
		if err != nil {
			return fmt.Errorf("rebuild session: %w", err)
		}
		_, err = fmt.Fprintf(options.Output, "domain: %s\nsession: %s\nstate: %s\nbase: %s\n", rebuilt.Domain, rebuilt.Name, rebuilt.IntendedState, rebuilt.GoldenRevision)
		return err
	case commandWorkspaceAttach:
		if options.Observer == nil {
			return errors.New("backend observer is required")
		}
		record, err := workspacex.Attach(ctx, selectedDomain.StateRoot, selectedDomain.ID, command.volumeID, command.name, command.mountPath, options.Observer)
		if err != nil {
			return fmt.Errorf("attach workspace: %w", err)
		}
		return writeWorkspaceAttachment(options.Output, record, "attached")
	case commandWorkspaceDetach:
		if options.Observer == nil {
			return errors.New("backend observer is required")
		}
		record, err := workspacex.Detach(ctx, selectedDomain.StateRoot, selectedDomain.ID, command.volumeID, command.name, options.Observer)
		if err != nil {
			return fmt.Errorf("detach workspace: %w", err)
		}
		return writeWorkspaceAttachment(options.Output, record, "detached")
	case commandWorkspaceExport:
		if options.Observer == nil || options.AlphaExport == nil {
			return errors.New("workspace export requires backend observation and alpha exporter")
		}
		if selectedDomain.ID != "alpha" {
			return errors.New("v0.2 workspace export is limited to the explicit alpha domain")
		}
		journal, published, err := options.AlphaExport(ctx, selectedDomain, command.alphaExport, options.Observer)
		if err != nil {
			return fmt.Errorf("export workspace: %w", err)
		}
		return writeAlphaExport(options.Output, selectedDomain, journal, published)
	case commandWorkspaceExportResume:
		if options.AlphaExportResume == nil {
			return errors.New("workspace export resume requires alpha recovery composition")
		}
		if selectedDomain.ID != "alpha" {
			return errors.New("v0.2 workspace export resume is limited to the explicit alpha domain")
		}
		journal, published, err := options.AlphaExportResume(ctx, selectedDomain, command.alphaExportResume)
		if err != nil {
			return fmt.Errorf("resume workspace export: %w", err)
		}
		return writeAlphaExport(options.Output, selectedDomain, journal, published)
	default:
		return errors.New("unsupported command")
	}
}

type commandKind uint8

const (
	commandSessionStatus commandKind = iota + 1
	commandGoldenRegister
	commandSessionCreate
	commandSessionStart
	commandSessionStop
	commandSessionRebuild
	commandInit
	commandDoctor
	commandDomainInit
	commandAlphaRecipeCheck
	commandAlphaPrepare
	commandWorkspaceAttach
	commandWorkspaceDetach
	commandWorkspaceExport
	commandWorkspaceExportResume
)

type parsedCommand struct {
	kind              commandKind
	configPath        string
	domain            string
	name              string
	mode              session.Mode
	recipePath        string
	isoPath           string
	alphaPrepare      AlphaPrepareInput
	alphaExport       AlphaExportInput
	alphaExportResume AlphaExportResumeInput
	recipeCreate      bool
	rebuildBase       string
	volumeID          string
	mountPath         string
}

func (c parsedCommand) requiresDomain() bool {
	return c.kind != commandInit && c.kind != commandDoctor
}

func (c parsedCommand) requiresBackend() bool {
	return c.kind == commandGoldenRegister || c.kind == commandSessionCreate || c.kind == commandSessionStatus || c.kind == commandWorkspaceAttach || c.kind == commandWorkspaceDetach || c.kind == commandWorkspaceExport
}

func parseCommand(args []string, options Options) (parsedCommand, error) {
	configPath := options.ConfigPath
	if configPath == "" {
		var err error
		configPath, err = DefaultConfigPath()
		if err != nil {
			return parsedCommand{}, err
		}
	}

	set := flag.NewFlagSet("boxwarden", flag.ContinueOnError)
	set.SetOutput(io.Discard)
	domain := set.String("domain", environmentValue(options.Env, "BOXWARDEN_DOMAIN"), "security domain")
	config := set.String("config", configPath, "configuration file")
	if err := set.Parse(args); err != nil {
		return parsedCommand{}, fmt.Errorf("parse command: %w", err)
	}
	if strings.TrimSpace(*config) == "" {
		return parsedCommand{}, errors.New("configuration path is required")
	}
	explicitDomain := false
	set.Visit(func(flag *flag.Flag) {
		if flag.Name == "domain" {
			explicitDomain = true
		}
	})

	remaining := set.Args()
	base := parsedCommand{configPath: *config}
	if len(remaining) == 1 && (remaining[0] == "init" || remaining[0] == "doctor") {
		if explicitDomain {
			return parsedCommand{}, fmt.Errorf("--domain is not accepted for host-global command %q", remaining[0])
		}
		if remaining[0] == "init" {
			base.kind = commandInit
		} else {
			base.kind = commandDoctor
		}
		return base, nil
	}
	if strings.TrimSpace(*domain) == "" {
		return parsedCommand{}, errors.New("domain is required; pass --domain or set BOXWARDEN_DOMAIN")
	}
	base.domain = *domain
	if len(remaining) >= 2 && remaining[0] == "alpha" && remaining[1] == "prepare" {
		prepareSet := flag.NewFlagSet("alpha prepare", flag.ContinueOnError)
		prepareSet.SetOutput(io.Discard)
		input := AlphaPrepareInput{}
		bindAlphaPrepareFlags(prepareSet, &input)
		if err := prepareSet.Parse(remaining[2:]); err != nil {
			return parsedCommand{}, fmt.Errorf("parse alpha prepare: %w", err)
		}
		if len(prepareSet.Args()) != 0 {
			return parsedCommand{}, errors.New("alpha prepare accepts only named inputs")
		}
		if err := validAlphaPrepareInput(input); err != nil {
			return parsedCommand{}, err
		}
		base.kind, base.alphaPrepare = commandAlphaPrepare, input
		return base, nil
	}
	if len(remaining) >= 3 && remaining[0] == "alpha" && remaining[1] == "recipe" && remaining[2] == "check" {
		checkSet := flag.NewFlagSet("alpha recipe check", flag.ContinueOnError)
		checkSet.SetOutput(io.Discard)
		recipePath := checkSet.String("recipe", "", "versioned recipe JSON")
		isoPath := checkSet.String("iso", "", "local installer ISO")
		if err := checkSet.Parse(remaining[3:]); err != nil {
			return parsedCommand{}, fmt.Errorf("parse alpha recipe check: %w", err)
		}
		if len(checkSet.Args()) != 0 || *recipePath == "" || *isoPath == "" {
			return parsedCommand{}, errors.New("alpha recipe check requires --recipe PATH and --iso PATH")
		}
		base.kind = commandAlphaRecipeCheck
		base.recipePath, base.isoPath = *recipePath, *isoPath
		return base, nil
	}
	if len(remaining) == 3 && remaining[0] == "session" && remaining[1] == "status" {
		base.kind = commandSessionStatus
		base.name = remaining[2]
		return base, nil
	}
	if len(remaining) == 2 && remaining[0] == "domain" && remaining[1] == "init" {
		base.kind = commandDomainInit
		return base, nil
	}
	if len(remaining) == 3 && remaining[0] == "golden" && remaining[1] == "register" {
		base.kind = commandGoldenRegister
		base.name = remaining[2]
		return base, nil
	}
	if len(remaining) >= 3 && remaining[0] == "session" && remaining[1] == "create" {
		createSet := flag.NewFlagSet("session create", flag.ContinueOnError)
		createSet.SetOutput(io.Discard)
		mode := createSet.String("mode", string(session.ModeClean), "session mode")
		input := AlphaPrepareInput{}
		bindAlphaPrepareFlags(createSet, &input)
		if err := createSet.Parse(remaining[2:]); err != nil {
			return parsedCommand{}, fmt.Errorf("parse session create: %w", err)
		}
		if len(createSet.Args()) != 1 {
			return parsedCommand{}, errors.New("session create requires one validated session name")
		}
		base.mode = session.Mode(*mode)
		if base.mode != session.ModeClean && base.mode != session.ModeQuarantine {
			return parsedCommand{}, fmt.Errorf("invalid session mode %q", base.mode)
		}
		if hasAlphaPrepareFlags(createSet) {
			if err := validAlphaPrepareInput(input); err != nil {
				return parsedCommand{}, err
			}
			base.recipeCreate, base.alphaPrepare = true, input
		}
		base.kind = commandSessionCreate
		base.name = createSet.Args()[0]
		return base, nil
	}
	if len(remaining) == 3 && remaining[0] == "session" && remaining[1] == "start" {
		base.kind = commandSessionStart
		base.name = remaining[2]
		return base, nil
	}
	if len(remaining) == 3 && remaining[0] == "session" && remaining[1] == "stop" {
		base.kind = commandSessionStop
		base.name = remaining[2]
		return base, nil
	}
	if len(remaining) >= 3 && remaining[0] == "session" && remaining[1] == "rebuild" {
		rebuildSet := flag.NewFlagSet("session rebuild", flag.ContinueOnError)
		rebuildSet.SetOutput(io.Discard)
		baseRevision := rebuildSet.String("base", "", "exact admitted replacement base revision")
		input := AlphaPrepareInput{}
		bindAlphaPrepareFlags(rebuildSet, &input)
		if err := rebuildSet.Parse(remaining[2:]); err != nil {
			return parsedCommand{}, fmt.Errorf("parse session rebuild: %w", err)
		}
		if len(rebuildSet.Args()) != 1 {
			return parsedCommand{}, errors.New("session rebuild requires one validated session name")
		}
		hasRecipe := hasAlphaPrepareFlags(rebuildSet)
		if *baseRevision != "" && hasRecipe {
			return parsedCommand{}, errors.New("session rebuild accepts either --base or recipe inputs")
		}
		if *baseRevision != "" {
			if err := backend.ValidateObjectID(*baseRevision); err != nil {
				return parsedCommand{}, fmt.Errorf("invalid rebuild base: %w", err)
			}
		}
		if hasRecipe {
			if err := validAlphaPrepareInput(input); err != nil {
				return parsedCommand{}, err
			}
		}
		base.kind, base.name, base.rebuildBase = commandSessionRebuild, rebuildSet.Args()[0], *baseRevision
		base.recipeCreate, base.alphaPrepare = hasRecipe, input
		return base, nil
	}
	if len(remaining) >= 3 && remaining[0] == "workspace" && remaining[1] == "attach" {
		attachSet := flag.NewFlagSet("workspace attach", flag.ContinueOnError)
		attachSet.SetOutput(io.Discard)
		mount := attachSet.String("mount", "", "fixed guest workspace mount path")
		if err := attachSet.Parse(remaining[2:]); err != nil {
			return parsedCommand{}, fmt.Errorf("parse workspace attach: %w", err)
		}
		if len(attachSet.Args()) != 2 || *mount == "" {
			return parsedCommand{}, errors.New("workspace attach requires --mount PATH <volume-uuid> <stopped-session>")
		}
		base.kind, base.volumeID, base.name, base.mountPath = commandWorkspaceAttach, attachSet.Args()[0], attachSet.Args()[1], *mount
		return base, nil
	}
	if len(remaining) == 4 && remaining[0] == "workspace" && remaining[1] == "detach" {
		base.kind, base.volumeID, base.name = commandWorkspaceDetach, remaining[2], remaining[3]
		return base, nil
	}
	if len(remaining) >= 4 && remaining[0] == "workspace" && remaining[1] == "export" && remaining[2] == "resume" {
		resumeSet := flag.NewFlagSet("workspace export resume", flag.ContinueOnError)
		resumeSet.SetOutput(io.Discard)
		input := AlphaExportResumeInput{}
		resumeSet.StringVar(&input.SourceRoot, "source-root", "", "clean inspector source checkout")
		resumeSet.StringVar(&input.ISOPath, "iso", "", "pinned Ubuntu ARM64 installer ISO")
		resumeSet.StringVar(&input.GoBinary, "go", "", "absolute Go executable")
		if err := resumeSet.Parse(remaining[3:]); err != nil {
			return parsedCommand{}, fmt.Errorf("parse workspace export resume: %w", err)
		}
		if len(resumeSet.Args()) != 1 {
			return parsedCommand{}, errors.New("workspace export resume requires one transaction UUID")
		}
		input.TransactionID = resumeSet.Args()[0]
		if err := validAlphaExportResumeInput(input); err != nil {
			return parsedCommand{}, err
		}
		base.kind, base.alphaExportResume = commandWorkspaceExportResume, input
		return base, nil
	}
	if len(remaining) >= 3 && remaining[0] == "workspace" && remaining[1] == "export" {
		exportSet := flag.NewFlagSet("workspace export", flag.ContinueOnError)
		exportSet.SetOutput(io.Discard)
		input := AlphaExportInput{}
		exportSet.StringVar(&input.DestinationParent, "destination", "", "existing empty private destination directory")
		exportSet.StringVar(&input.SourceRoot, "source-root", "", "clean inspector source checkout")
		exportSet.StringVar(&input.ISOPath, "iso", "", "pinned Ubuntu ARM64 installer ISO")
		exportSet.StringVar(&input.GoBinary, "go", "", "absolute Go executable")
		selected := exportSelections{}
		exportSet.Var(&selected, "select", "relative workspace file or directory; repeatable")
		if err := exportSet.Parse(remaining[2:]); err != nil {
			return parsedCommand{}, fmt.Errorf("parse workspace export: %w", err)
		}
		if len(exportSet.Args()) != 1 {
			return parsedCommand{}, errors.New("workspace export requires one volume UUID")
		}
		input.VolumeID, input.Selected = exportSet.Args()[0], append([]string(nil), selected...)
		if err := validAlphaExportInput(input); err != nil {
			return parsedCommand{}, err
		}
		base.kind, base.alphaExport = commandWorkspaceExport, input
		return base, nil
	}
	return parsedCommand{}, errors.New("supported commands are: init, doctor, domain init, golden register <object>, session create [--mode clean|quarantine] [--recipe PATH --iso PATH --guest-definition PATH --openssl PATH --openssl-sha256 SHA256 --xorriso PATH --xorriso-sha256 SHA256] <session>, session start <session>, session stop <session>, session rebuild [--base REVISION | recipe inputs] <session>, session status <session>, workspace attach --mount PATH <volume-uuid> <stopped-session>, workspace detach <volume-uuid> <stopped-session>, workspace export --destination PATH --select RELATIVE [--select RELATIVE...] --source-root PATH --iso PATH --go PATH <volume-uuid>, workspace export resume --source-root PATH --iso PATH --go PATH <transaction-uuid>, alpha recipe check --recipe PATH --iso PATH, alpha prepare --recipe PATH --iso PATH --guest-definition PATH --openssl PATH --openssl-sha256 SHA256 --xorriso PATH --xorriso-sha256 SHA256")
}

func writeWorkspaceAttachment(output io.Writer, record workspacex.Record, state string) error {
	if _, err := fmt.Fprintf(output, "domain: %s\nvolume: %s\nworkspace: %s\n", record.Domain, record.VolumeID, state); err != nil {
		return fmt.Errorf("write workspace attachment result: %w", err)
	}
	return nil
}

func writeInit(output io.Writer, result hostx.InitResult) error {
	if _, err := fmt.Fprintf(output, "host-installed: %t\nrefresh-login-session: %t\n", result.HostInstalled, result.RefreshLoginSession); err != nil {
		return fmt.Errorf("write init result: %w", err)
	}
	return nil
}

func writeDomainInit(output io.Writer, domain string, disposition sshx.CAInitDisposition) error {
	var status string
	switch disposition {
	case sshx.CAInitialized:
		status = "initialized"
	case sshx.CAAlreadyInitialized:
		status = "already initialized"
	default:
		return fmt.Errorf("domain CA initializer returned unknown disposition %q", disposition)
	}
	if _, err := fmt.Fprintf(output, "domain: %s\nmanagement-ca: %s\n", domain, status); err != nil {
		return fmt.Errorf("write domain init result: %w", err)
	}
	return nil
}

func hostRequest(loaded config.Config) (hostx.Request, error) {
	admission, err := loaded.HostAdmission()
	if err != nil {
		return hostx.Request{}, err
	}
	return hostx.Request{ConfiguredStateRoots: admission.ConfiguredStateRoots, TartPath: admission.Host.TartExecutable, TartHome: admission.Host.TartHome, SoftnetPath: admission.Host.SoftnetSource}, nil
}

func configuredCADomains(loaded config.Config) []sshx.Domain {
	configured := loaded.Domains()
	domains := make([]sshx.Domain, 0, len(configured))
	for _, configuredDomain := range configured {
		domains = append(domains, sshx.Domain{ID: configuredDomain.ID, StateRoot: configuredDomain.StateRoot})
	}
	return domains
}

func writeDoctor(output io.Writer, report hostx.Report) error {
	report.Normalize()
	if _, err := fmt.Fprintf(output, "status: %s\n", report.Status); err != nil {
		return fmt.Errorf("write doctor report: %w", err)
	}
	for _, finding := range report.Findings {
		if _, err := fmt.Fprintf(output, "%s: [%s] observed=%s expected=%s remedy=%s\n", finding.Code, finding.Category, finding.Observed, finding.Expected, finding.Remedy); err != nil {
			return fmt.Errorf("write doctor finding: %w", err)
		}
	}
	return nil
}

func environmentValue(environment []string, key string) string {
	if environment == nil {
		environment = os.Environ()
	}
	prefix := key + "="
	for _, entry := range environment {
		if strings.HasPrefix(entry, prefix) {
			return strings.TrimPrefix(entry, prefix)
		}
	}
	return ""
}

const maxStatusSnapshotAge = 5 * time.Second

func reconcileStatusSnapshot(ctx context.Context, loaded config.Config, selected config.Domain, record session.Record, observed backend.Observation, reconciled lifecycle.Reconciliation, factory StatusSnapshotFactory) (lifecycle.Reconciliation, session.ReadinessStatus) {
	if record.IntendedState != session.StateRunning {
		return reconciled, ""
	}
	if !observed.Exists || observed.ObjectID != record.Backend.ObjectID || (observed.State != backend.ObjectRunning && observed.State != backend.ObjectStopped) {
		return reconciled, session.ReadinessDrift
	}
	if record.Readiness.Status != session.ReadinessReady {
		return lifecycle.Reconciliation{Consistency: lifecycle.Drift, Diagnostic: "durable readiness requires explicit lifecycle reconciliation"}, session.ReadinessDrift
	}
	if factory == nil {
		return lifecycle.Reconciliation{Consistency: lifecycle.Drift, Diagnostic: "exact live supervisor readiness is unavailable; backend is not adopted"}, session.ReadinessDrift
	}
	reader, err := factory(loaded, selected)
	if err != nil || reader == nil {
		return lifecycle.Reconciliation{Consistency: lifecycle.Drift, Diagnostic: "exact live supervisor readiness is unavailable; backend is not adopted"}, session.ReadinessDrift
	}
	binding := supervisor.Binding{Domain: string(record.Domain), SessionID: record.ID, BackendKind: record.Backend.Kind, BackendObject: record.Backend.ObjectID, Generation: record.StartGeneration}
	snapshot, err := reader.Snapshot(ctx, binding)
	if err != nil {
		return lifecycle.Reconciliation{Consistency: lifecycle.Drift, Diagnostic: "exact live supervisor snapshot is unavailable; backend is not adopted"}, session.ReadinessDrift
	}
	now := time.Now()
	if snapshot.Binding != binding || snapshot.ObservedAt.IsZero() || snapshot.ObservedAt.After(now) || now.Sub(snapshot.ObservedAt) > maxStatusSnapshotAge {
		return lifecycle.Reconciliation{Consistency: lifecycle.Drift, Diagnostic: "exact live supervisor snapshot is stale or mismatched"}, session.ReadinessDrift
	}
	if !snapshot.BackendRunning || !snapshot.SerialHealthy || !snapshot.PinPresent || !snapshot.CertificateCurrent || !snapshot.ProbeOK || !snapshot.ZoneMatches {
		return lifecycle.Reconciliation{Consistency: lifecycle.Drift, Diagnostic: "exact live supervisor readiness checks failed"}, session.ReadinessDrift
	}
	if observed.State == backend.ObjectStopped {
		return lifecycle.Reconciliation{Consistency: lifecycle.Consistent, Diagnostic: "Tart listing reports stopped; exact retained supervisor generation passed fresh readiness checks"}, session.ReadinessReady
	}
	return lifecycle.Reconciliation{Consistency: lifecycle.Consistent}, session.ReadinessReady
}

func writeStatus(output io.Writer, record session.Record, observed backend.Observation, reconciled lifecycle.Reconciliation, readiness session.ReadinessStatus) error {
	observedState := string(observed.State)
	if !observed.Exists {
		observedState = "missing"
	}
	golden := record.GoldenRevision
	if golden == "" {
		golden = "(none)"
	}
	if _, err := fmt.Fprintf(output, "domain: %s\nsession: %s\nmode: %s\nintended: %s\nobserved: %s\ngolden: %s\nconsistency: %s\n", record.Domain, record.Name, record.Mode, record.IntendedState, observedState, golden, reconciled.Consistency); err != nil {
		return fmt.Errorf("write status: %w", err)
	}
	if readiness != "" {
		if _, err := fmt.Fprintf(output, "readiness: %s\n", readiness); err != nil {
			return fmt.Errorf("write live readiness: %w", err)
		}
	}
	if observed.Diagnostic != "" {
		if _, err := fmt.Fprintf(output, "backend-diagnostic: %s\n", observed.Diagnostic); err != nil {
			return fmt.Errorf("write backend diagnostic: %w", err)
		}
	}
	if reconciled.Diagnostic != "" {
		if _, err := fmt.Fprintf(output, "diagnostic: %s\n", reconciled.Diagnostic); err != nil {
			return fmt.Errorf("write reconciliation diagnostic: %w", err)
		}
	}
	return nil
}

func writeGoldenRegistration(output io.Writer, record golden.Record) error {
	if _, err := fmt.Fprintf(output, "domain: %s\ngolden: %s\nstate: registered\n", record.Domain, record.Revision); err != nil {
		return fmt.Errorf("write golden registration: %w", err)
	}
	return nil
}

func writeCreatedSession(output io.Writer, record session.Record) error {
	if _, err := fmt.Fprintf(output, "domain: %s\nsession: %s\nmode: %s\nstate: %s\n", record.Domain, record.Name, record.Mode, record.IntendedState); err != nil {
		return fmt.Errorf("write created session: %w", err)
	}
	return nil
}

func writeStartedSession(output io.Writer, record session.Record) error {
	if _, err := fmt.Fprintf(output, "domain: %s\nsession: %s\nstate: %s\nreadiness: %s\n", record.Domain, record.Name, record.IntendedState, record.Readiness.Status); err != nil {
		return fmt.Errorf("write started session: %w", err)
	}
	return nil
}

func writeStoppedSession(output io.Writer, record session.Record) error {
	if _, err := fmt.Fprintf(output, "domain: %s\nsession: %s\nstate: %s\nreadiness: %s\n", record.Domain, record.Name, record.IntendedState, record.Readiness.Status); err != nil {
		return fmt.Errorf("write stopped session: %w", err)
	}
	return nil
}
