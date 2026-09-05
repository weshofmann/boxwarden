package tart

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"unicode"

	"github.com/weshofmann/boxwarden/internal/backend"
)

// LaunchConfig carries the V3-admitted Tart/Softnet and operator facts. Start
// policy is fixed here rather than being accepted from lifecycle callers.
type LaunchConfig struct {
	TartPath      string
	TartHome      string
	SoftnetBinDir string
	OperatorHome  string
	OperatorName  string
}

// processSpec is one bounded direct-child invocation, constructed only by the
// fixed Launcher policy. It is deliberately not an adapter API.
type processSpec struct {
	path string
	args []string
	env  []string
	dir  string
}

// processStarter is private so callers cannot use the Tart lifecycle adapter
// as a generic direct-execution facility.
type processStarter interface {
	start(context.Context, processSpec) (backend.Handle, error)
}

// Launcher starts exactly one existing Tart object using the qualified launch
// policy. It contains no generic Tart command capability.
type Launcher struct {
	config  LaunchConfig
	process processStarter
}

// NewLauncher installs the only production direct-child implementation. Its
// public input consists exclusively of already-admitted host facts.
func NewLauncher(config LaunchConfig) Launcher { return newLauncher(config, osProcessStarter{}) }

func newLauncher(config LaunchConfig, process processStarter) Launcher {
	return Launcher{config: config, process: process}
}

func (l Launcher) Start(ctx context.Context, request backend.StartRequest) (backend.Handle, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := backend.ValidateStartRequest(request); err != nil {
		return nil, fmt.Errorf("start Tart: %w", err)
	}
	if err := validateLaunchConfig(l.config); err != nil {
		return nil, fmt.Errorf("start Tart: %w", err)
	}
	if l.process == nil {
		return nil, fmt.Errorf("start Tart: process starter is required")
	}
	spec := processSpec{
		path: l.config.TartPath,
		args: []string{"run", "--net-softnet", "--no-audio", "--no-clipboard", "--serial-path", request.SerialDevice, request.ObjectID},
		env: []string{
			"PATH=" + l.config.SoftnetBinDir,
			"HOME=" + l.config.OperatorHome,
			"USER=" + l.config.OperatorName,
			"LOGNAME=" + l.config.OperatorName,
			"TART_HOME=" + l.config.TartHome,
			"TMPDIR=" + request.GenerationDirectory,
			"LANG=C",
			"LC_ALL=C",
		},
		dir: request.GenerationDirectory,
	}
	handle, err := l.process.start(ctx, spec)
	if err != nil {
		return nil, fmt.Errorf("start Tart process: %w", err)
	}
	if handle == nil {
		return nil, fmt.Errorf("start Tart process: no owned handle returned")
	}
	return handle, nil
}

func validateLaunchConfig(config LaunchConfig) error {
	for name, path := range map[string]string{
		"Tart path": config.TartPath, "Tart home": config.TartHome, "Softnet PATH": config.SoftnetBinDir, "operator home": config.OperatorHome,
	} {
		if !canonicalAbsolutePath(path) {
			return fmt.Errorf("%s must be canonical and absolute", name)
		}
	}
	if !safeEnvironmentValue(config.OperatorName) {
		return fmt.Errorf("operator name is invalid")
	}
	return nil
}

func canonicalAbsolutePath(path string) bool {
	return path != "/" && filepath.IsAbs(path) && filepath.Clean(path) == path && strings.IndexFunc(path, unicode.IsControl) < 0
}

func safeEnvironmentValue(value string) bool {
	return value != "" && strings.IndexFunc(value, func(r rune) bool { return unicode.IsControl(r) || r == '=' }) < 0
}

var _ backend.Starter = Launcher{}

// osProcessStarter starts owned Tart processes without CommandContext: the
// supervisor, rather than a transient launch request context, owns shutdown.
// It is private because the fixed Launcher is the only allowed caller.
type osProcessStarter struct {
	spawn func(*exec.Cmd) error
}

func (s osProcessStarter) start(ctx context.Context, spec processSpec) (backend.Handle, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !supportsOwnedProcessGroups() {
		return nil, fmt.Errorf("start Tart process: owned process groups are unsupported on this platform")
	}
	command := newOwnedCommand(spec)
	spawn := s.spawn
	if spawn == nil {
		spawn = func(command *exec.Cmd) error { return command.Start() }
	}
	if err := spawn(command); err != nil {
		return nil, err
	}
	return &osProcessHandle{command: command, done: make(chan struct{}), signalGroup: signalOwnedProcessGroup, waitCommand: command.Wait}, nil
}

func newOwnedCommand(spec processSpec) *exec.Cmd {
	command := exec.Command(spec.path, spec.args...)
	command.Env = append([]string(nil), spec.env...)
	command.Dir = spec.dir
	configureOwnedProcessGroup(command)
	return command
}

type osProcessHandle struct {
	command     *exec.Cmd
	done        chan struct{}
	signalGroup func(int) error
	stopOnce    sync.Once
	stopErr     error
	waitOnce    sync.Once
	waitErr     error
	waitCommand func() error
}

func (h *osProcessHandle) Stop(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if h == nil || h.command == nil || h.command.Process == nil {
		return fmt.Errorf("owned Tart process is unavailable")
	}
	select {
	case <-h.done:
		return nil
	default:
	}
	signalGroup := h.signalGroup
	if signalGroup == nil {
		signalGroup = signalOwnedProcessGroup
	}
	h.stopOnce.Do(func() { h.stopErr = signalGroup(-h.command.Process.Pid) })
	return h.stopErr
}

func (h *osProcessHandle) Wait(ctx context.Context) error {
	if h == nil || h.command == nil {
		return fmt.Errorf("owned Tart process is unavailable")
	}
	select {
	case <-h.done:
		return h.waitErr
	default:
	}
	h.waitOnce.Do(func() {
		go func() {
			waitCommand := h.waitCommand
			if waitCommand == nil {
				waitCommand = h.command.Wait
			}
			h.waitErr = waitCommand()
			close(h.done)
		}()
	})
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-h.done:
		return h.waitErr
	}
}
