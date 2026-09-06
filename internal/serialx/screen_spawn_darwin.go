//go:build darwin && cgo

package serialx

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"syscall"
)

type execScreenCommand struct{ cmd *exec.Cmd }

func (c *execScreenCommand) PID() int      { return c.cmd.Process.Pid }
func (c *execScreenCommand) Signal() error { return c.cmd.Process.Signal(syscall.SIGTERM) }
func (c *execScreenCommand) Kill() error   { return c.cmd.Process.Kill() }
func (c *execScreenCommand) Wait() error   { return c.cmd.Wait() }

func productionRuntimeDeps() (runtimeDeps, error) {
	return runtimeDeps{
		allocatePTY:     func() (*os.File, *os.File, error) { return allocatePTY() },
		startScreen:     startProductionScreen,
		qualifiedScreen: qualifiedScreen,
	}, nil
}

func productionRuntimeSupported() bool { return true }

func startProductionScreen(ctx context.Context, launch screenLaunch) (*ownedScreen, error) {
	if launch.path != ScreenPath || len(launch.args) != 4 || !sameStrings(launch.args[:3], []string{"-D", "-m", "-S"}) || launch.args[3] == "" || launch.stdin == nil {
		return nil, fmt.Errorf("invalid fixed Screen launch")
	}
	return startOwnedScreen(ctx, launch, screenStartDeps{
		start: func(ctx context.Context, launch screenLaunch) (startedScreen, error) {
			if err := ctx.Err(); err != nil {
				return startedScreen{}, err
			}
			cmd := exec.Command(launch.path, launch.args...)
			cmd.Stdin = launch.stdin
			if err := cmd.Start(); err != nil {
				return startedScreen{}, err
			}
			return startedScreen{cmd: cmd, direct: &execScreenCommand{cmd: cmd}}, nil
		},
		observe: observeScreenIdentity,
	})
}
