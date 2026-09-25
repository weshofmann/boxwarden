package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/weshofmann/boxwarden/internal/alphaprep"
	"github.com/weshofmann/boxwarden/internal/app"
	"github.com/weshofmann/boxwarden/internal/backend"
	"github.com/weshofmann/boxwarden/internal/backend/tart"
	"github.com/weshofmann/boxwarden/internal/basebuild"
	"github.com/weshofmann/boxwarden/internal/config"
	"github.com/weshofmann/boxwarden/internal/execx"
	"github.com/weshofmann/boxwarden/internal/hostx"
	"github.com/weshofmann/boxwarden/internal/importx"
	"github.com/weshofmann/boxwarden/internal/recipe"
	"github.com/weshofmann/boxwarden/internal/session"
	"github.com/weshofmann/boxwarden/internal/sessionruntime"
	"github.com/weshofmann/boxwarden/internal/sshx"
	"github.com/weshofmann/boxwarden/internal/supervisor"
	"github.com/weshofmann/boxwarden/internal/workspaceformat"
	"github.com/weshofmann/boxwarden/internal/workspacex"
)

type rootInstaller func(context.Context, []byte) ([]byte, error)

func main() {
	ctx := context.Background()
	handled, err := runInternal(ctx, os.Args[1:], os.Stdin, os.Stdout, hostx.RunRootHostInstall, sessionruntime.RunRequest)
	if handled {
		finish(err)
		return
	}

	publicCtx, stopSignals := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	err = app.Run(publicCtx, os.Args[1:], publicOptions(os.Stdout))
	stopSignals()
	finish(err)
}

func publicOptions(output io.Writer) app.Options {
	sshRunner := sshx.NewExecRunner()
	caStore := sshx.NewCAStore(sshx.CAStoreOptions{
		Runner:        sshRunner,
		Identity:      sshx.OSIdentity{},
		NewUUID:       sshx.RandomUUID,
		SSHKeygenPath: "/usr/bin/ssh-keygen",
	})
	hostInitializer := hostx.NewSystemInitializer()
	hostDoctor := hostx.NewSystemDoctor()
	return app.Options{
		BackendFactory: func(loaded config.Config, selected config.Domain) (app.BackendDependencies, error) {
			configured, err := loaded.Domain(string(selected.ID))
			if err != nil || configured != selected {
				return app.BackendDependencies{}, fmt.Errorf("backend requires exact configured domain")
			}
			host, err := loaded.Host()
			if err != nil {
				return app.BackendDependencies{}, err
			}
			adapter := tart.NewQualifiedObserver(execx.OSRunner{MaxOutputBytes: 1 << 20}, host.TartExecutable, host.TartHome)
			return app.BackendDependencies{Observer: adapter, Creator: adapter}, nil
		},
		HostInit:   hostInitializer,
		HostDoctor: hostDoctor,
		CAInit:     caStore,
		SessionStarterFactory: func(loaded config.Config, selected config.Domain, path string) (app.SessionStarter, error) {
			return sessionruntime.NewStarter(loaded, selected, path)
		},
		SessionStopperFactory: func(loaded config.Config, selected config.Domain, path string) (app.SessionStopper, error) {
			return sessionruntime.NewStarter(loaded, selected, path)
		},
		AlphaRebuild: sessionruntime.Rebuild,
		AlphaDelete:  sessionruntime.Delete,
		AlphaAction: func(ctx context.Context, selected config.Domain, input app.AlphaActionInput) (session.ActionAttempt, error) {
			controller, err := supervisor.NewExactActionController(filepath.Join(selected.StateRoot, "runtime"))
			if err != nil {
				return session.ActionAttempt{}, err
			}
			service := session.NewActionService(selected, controller)
			switch input.Operation {
			case "run":
				return service.ExecuteAction(ctx, input.SessionName, input.Phase, input.ActionID)
			case "retry":
				return service.RetryAction(ctx, input.SessionName, input.AttemptID)
			case "skip":
				return service.SkipAction(ctx, input.SessionName, input.AttemptID)
			default:
				return session.ActionAttempt{}, fmt.Errorf("unsupported alpha action operation")
			}
		},
		AlphaWorkspaceCreate: func(ctx context.Context, selected config.Domain, input app.AlphaWorkspaceCreateInput) (workspacex.Record, error) {
			formatter := workspaceformat.VZFormatter{StateRoot: selected.StateRoot, Domain: selected.ID,
				BundlePath: input.BundlePath, SourceRoot: input.SourceRoot}
			return workspacex.CreateManaged(ctx, selected.StateRoot, workspaceformat.Request{Domain: selected.ID,
				VolumeID: input.VolumeID, FilesystemUUID: input.FilesystemUUID, SizeBytes: input.SizeBytes}, formatter)
		},
		StatusSnapshotFactory: func(loaded config.Config, selected config.Domain) (app.StatusSnapshotReader, error) {
			configured, err := loaded.Domain(string(selected.ID))
			if err != nil || configured != selected {
				return nil, fmt.Errorf("status requires exact configured domain")
			}
			return supervisor.NewExactSnapshotReader(filepath.Join(selected.StateRoot, "runtime"))
		},
		AlphaPrepare: func(ctx context.Context, loaded config.Config, selected config.Domain, configPath string, input app.AlphaPrepareInput) (app.AlphaPrepared, error) {
			admitted, err := loaded.Domain(string(selected.ID))
			if err != nil || admitted != selected {
				return app.AlphaPrepared{}, fmt.Errorf("alpha preparation requires exact configured domain")
			}
			value, err := recipe.LoadRunnable(input.RecipePath)
			if err != nil {
				return app.AlphaPrepared{}, fmt.Errorf("load alpha recipe: %w", err)
			}
			request, err := alphaprep.NewRequest(selected, value, input.ISOPath, input.GuestDefinitionRoot)
			if err != nil {
				return app.AlphaPrepared{}, err
			}
			digest, err := session.PublishRecipeIntent(selected.StateRoot, value)
			if err != nil {
				return app.AlphaPrepared{}, fmt.Errorf("capture alpha recipe intent: %w", err)
			}
			if _, err := fmt.Fprintf(output, "preparation-attempt: %s\nplanned-candidate: %s\n", request.Inputs.AttemptID, request.Inputs.CandidateID); err != nil {
				return app.AlphaPrepared{}, fmt.Errorf("report alpha preparation plan: %w", err)
			}
			host, err := loaded.Host()
			if err != nil {
				return app.AlphaPrepared{}, err
			}
			runner := execx.OSRunner{MaxOutputBytes: 1 << 20}
			observer := tart.NewQualifiedObserver(runner, host.TartExecutable, host.TartHome)
			components := alphaprep.BuildComponents{Runner: runner, Observer: observer, ScriptRunner: basebuild.OSOwnedScriptRunner{}, Launcher: basebuild.OSInstallerLauncher{},
				OpenSSLPath: input.OpenSSLPath, OpenSSLSHA256: input.OpenSSLSHA256, XorrisoPath: input.XorrisoPath, XorrisoSHA256: input.XorrisoSHA256}
			base, err := alphaprep.Prepare(ctx, loaded, selected, configPath, request, hostDoctor, caStore, components)
			if err != nil {
				return app.AlphaPrepared{}, err
			}
			return app.AlphaPrepared{Base: base, IntentDigest: digest}, nil
		},
		AlphaExport: func(ctx context.Context, selected config.Domain, input app.AlphaExportInput, observer backend.Observer) (workspacex.ExportJournal, string, error) {
			return workspacex.ExportSelectedWorkspace(ctx, selected.StateRoot, selected.ID, input.VolumeID,
				input.DestinationParent, input.Selected, observer, input.SourceRoot, input.ISOPath, input.GoBinary)
		},
		AlphaExportResume: func(ctx context.Context, selected config.Domain, input app.AlphaExportResumeInput) (workspacex.ExportJournal, string, error) {
			return workspacex.ResumeSelectedWorkspace(ctx, selected.StateRoot, selected.ID, input.TransactionID,
				input.SourceRoot, input.ISOPath, input.GoBinary)
		},
		AlphaImport: func(ctx context.Context, selected config.Domain, input app.AlphaImportInput) (workspacex.ImportJournal, supervisor.ImportResult, error) {
			controller, err := supervisor.NewExactImportController(filepath.Join(selected.StateRoot, "runtime"))
			if err != nil {
				return workspacex.ImportJournal{}, supervisor.ImportResult{}, err
			}
			staging := filepath.Join(selected.StateRoot, "imports")
			if !input.Resume {
				if err := os.Mkdir(staging, 0o700); err != nil && !os.IsExist(err) {
					return workspacex.ImportJournal{}, supervisor.ImportResult{}, err
				}
				if _, err := importx.CaptureSource(ctx, input.SourcePath, staging, input.TransactionID); err != nil {
					return workspacex.ImportJournal{}, supervisor.ImportResult{}, err
				}
			}
			journal, err := workspacex.LoadImportJournal(selected.StateRoot, selected.ID, input.TransactionID)
			if os.IsNotExist(err) {
				journal, err = workspacex.BeginImport(ctx, selected.StateRoot, selected.ID, input.SessionName, input.VolumeID, input.TransactionID, controller)
			} else if err == nil && (journal.SessionName != input.SessionName || journal.VolumeID != input.VolumeID) {
				return workspacex.ImportJournal{}, supervisor.ImportResult{}, fmt.Errorf("import transaction differs from requested session or volume")
			}
			if err != nil {
				return workspacex.ImportJournal{}, supervisor.ImportResult{}, err
			}
			receipt, err := workspacex.TransferCapturedImport(ctx, selected.StateRoot, selected.ID, input.TransactionID, controller)
			if err != nil {
				return journal, supervisor.ImportResult{}, err
			}
			journal, err = workspacex.LoadImportJournal(selected.StateRoot, selected.ID, input.TransactionID)
			return journal, receipt, err
		},
		AlphaImportVerify: func(ctx context.Context, selected config.Domain, input app.AlphaImportVerifyInput, observer backend.Observer) (workspacex.ImportJournal, error) {
			return workspacex.VerifyStoppedImport(ctx, selected.StateRoot, selected.ID, input.TransactionID, input.ExportID, observer)
		},
		Output: output,
	}
}

func runInternal(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer, install rootInstaller, supervisorRun ...func(context.Context, string) error) (bool, error) {
	if len(args) == 0 || args[0] != "internal" {
		return false, nil
	}
	if len(args) == 3 && args[1] == "session-supervisor" {
		if len(supervisorRun) > 1 || (len(supervisorRun) == 1 && supervisorRun[0] == nil) {
			return true, fmt.Errorf("supervisor dependencies are required")
		}
		run := sessionruntime.RunRequest
		if len(supervisorRun) == 1 {
			run = supervisorRun[0]
		}
		return true, run(ctx, args[2])
	}
	if len(args) != 2 || args[1] != "host-install" {
		return true, fmt.Errorf("unsupported internal command")
	}
	if install == nil || stdout == nil {
		return true, fmt.Errorf("root host-install dependencies are required")
	}
	request, err := hostx.ReadRootInstallRequest(stdin)
	if err != nil {
		return true, err
	}
	result, err := install(ctx, request)
	if err != nil {
		return true, err
	}
	written, err := stdout.Write(result)
	if err != nil {
		return true, fmt.Errorf("write root host-install result: %w", err)
	}
	if written != len(result) {
		return true, fmt.Errorf("write root host-install result: %w", io.ErrShortWrite)
	}
	return true, nil
}

func finish(err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "boxwarden: %v\n", err)
		os.Exit(1)
	}
}
