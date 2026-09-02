package main

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/weshofmann/boxwarden/internal/app"
	"github.com/weshofmann/boxwarden/internal/backend/tart"
	"github.com/weshofmann/boxwarden/internal/execx"
	"github.com/weshofmann/boxwarden/internal/hostx"
	"github.com/weshofmann/boxwarden/internal/sshx"
)

type rootInstaller func(context.Context, []byte) ([]byte, error)

func main() {
	ctx := context.Background()
	handled, err := runInternal(ctx, os.Args[1:], os.Stdin, os.Stdout, hostx.RunRootHostInstall)
	if handled {
		finish(err)
		return
	}

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
	err = app.Run(ctx, os.Args[1:], app.Options{
		Observer:   backendAdapter,
		Creator:    backendAdapter,
		HostInit:   hostInitializer,
		HostDoctor: hostDoctor,
		CAInit:     caStore,
		Output:     os.Stdout,
	})
	finish(err)
}

func runInternal(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer, install rootInstaller) (bool, error) {
	if len(args) == 0 || args[0] != "internal" {
		return false, nil
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
