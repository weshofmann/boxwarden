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
	TartPath       string
	TartHome       string
	SoftnetBinDir  string
	OperatorHome   string
	OperatorName   string
	ProcessStarter ProcessStarter
}

// ProcessSpec is one bounded, direct-child invocation. It cannot carry a
// shell, inherited environment, stdin, or caller-selected arguments.
type ProcessSpec struct {
	Path string
	Args []string
	Env  []string
	Dir  string
}

// ProcessStarter creates a direct child and returns the resulting owned
// handle. It is separate from execx.Runner because Tart run is long-lived.
type ProcessStarter interface {
	Start(context.Context, ProcessSpec) (backend.Handle, error)
}

// Launcher starts exactly one existing Tart object using the qualified launch
// policy. It contains no generic Tart command capability.
type Launcher struct{ config LaunchConfig }

func NewLauncher(config LaunchConfig) Launcher { return Launcher{config: config} }

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
	spec := ProcessSpec{
		Path: l.config.TartPath,
		Args: []string{"run", "--net-softnet", "--no-audio", "--no-clipboard", "--serial-path", request.SerialDevice, request.ObjectID},
		Env: []string{
			"PATH=" + l.config.SoftnetBinDir,
			"HOME=" + l.config.OperatorHome,
			"USER=" + l.config.OperatorName,
			"LOGNAME=" + l.config.OperatorName,
			"TART_HOME=" + l.config.TartHome,
			"TMPDIR=" + request.GenerationDirectory,
			"LANG=C",
			"LC_ALL=C",
		},
		Dir: request.GenerationDirectory,
	}
	handle, err := l.config.ProcessStarter.Start(ctx, spec)
	if err != nil {
		return nil, fmt.Errorf("start Tart process: %w", err)
	}
	if handle == nil {
		return nil, fmt.Errorf("start Tart process: no owned handle returned")
	}
	return handle, nil
}

func validateLaunchConfig(config LaunchConfig) error {
	if config.ProcessStarter == nil {
		return fmt.Errorf("process starter is required")
	}
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

// OSProcessStarter starts owned Tart processes without CommandContext: the
// supervisor, rather than a transient launch request context, owns shutdown.
type OSProcessStarter struct{}

func NewOSProcessStarter() OSProcessStarter { return OSProcessStarter{} }

func (OSProcessStarter) Start(ctx context.Context, spec ProcessSpec) (backend.Handle, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	command := newOwnedCommand(spec)
	if err := command.Start(); err != nil {
		return nil, err
	}
	return &osProcessHandle{command: command, done: make(chan struct{}), signalGroup: signalOwnedProcessGroup}, nil
}

func newOwnedCommand(spec ProcessSpec) *exec.Cmd {
	command := exec.Command(spec.Path, spec.Args...)
	command.Env = append([]string(nil), spec.Env...)
	command.Dir = spec.Dir
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
			h.waitErr = h.command.Wait()
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
