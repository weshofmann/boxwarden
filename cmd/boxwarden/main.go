package main

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/weshofmann/boxwarden/internal/app"
	"github.com/weshofmann/boxwarden/internal/backend/tart"
	"github.com/weshofmann/boxwarden/internal/config"
	"github.com/weshofmann/boxwarden/internal/execx"
	"github.com/weshofmann/boxwarden/internal/hostx"
	"github.com/weshofmann/boxwarden/internal/sessionruntime"
	"github.com/weshofmann/boxwarden/internal/sshx"
)

type rootInstaller func(context.Context, []byte) ([]byte, error)

func main() {
	ctx := context.Background()
	handled, err := runInternal(ctx, os.Args[1:], os.Stdin, os.Stdout, hostx.RunRootHostInstall, sessionruntime.RunRequest)
	if handled {
		finish(err)
		return
	}

	err = app.Run(ctx, os.Args[1:], publicOptions(os.Stdout))
	finish(err)
}

func publicOptions(output io.Writer) app.Options {
	backendAdapter := tart.New(execx.OSRunner{MaxOutputBytes: 1 << 20}, "tart")
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
		Observer:   backendAdapter,
		Creator:    backendAdapter,
		HostInit:   hostInitializer,
		HostDoctor: hostDoctor,
		CAInit:     caStore,
		SessionStarterFactory: func(loaded config.Config, selected config.Domain, path string) (app.SessionStarter, error) {
			return sessionruntime.NewStarter(loaded, selected, path)
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
