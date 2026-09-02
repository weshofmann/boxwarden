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

// LaunchConfig is the already-admitted qualified host toolchain and operator
// identity. Per-start policy is intentionally not configurable here.
type LaunchConfig struct {
	TartPath      string
	SoftnetBinDir string
	Home          string
	User          string
	Logname       string
	TartHome      string
}

// ProcessSpec is a bounded direct-child invocation. It has no shell, stdin,
// arbitrary flags, or inherited environment surface.
type ProcessSpec struct {
	Path string
	Args []string
	Env  []string
	Dir  string
}

// ProcessStarter starts a direct waitable child. It is intentionally separate
// from execx.Runner because Tart run is a long-lived owned process.
type ProcessStarter interface {
	Start(context.Context, ProcessSpec) (backend.Handle, error)
}

// Launcher starts Tart with the only qualified V4 session launch policy.
type Launcher struct {
	process ProcessStarter
	config  LaunchConfig
}

func NewLauncher(process ProcessStarter, config LaunchConfig) Launcher {
	return Launcher{process: process, config: config}
}

func (l Launcher) Start(ctx context.Context, request backend.StartRequest) (backend.Handle, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if l.process == nil {
		return nil, fmt.Errorf("start Tart: process starter is required")
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
			"HOME=" + l.config.Home,
			"USER=" + l.config.User,
			"LOGNAME=" + l.config.Logname,
			"TART_HOME=" + l.config.TartHome,
			"TMPDIR=" + request.GenerationDirectory,
			"LANG=C",
			"LC_ALL=C",
		},
		Dir: request.GenerationDirectory,
	}
	handle, err := l.process.Start(ctx, spec)
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
		"Tart path": config.TartPath, "Softnet PATH": config.SoftnetBinDir, "HOME": config.Home, "TART_HOME": config.TartHome,
	} {
		if !canonicalAbsolutePath(path) {
			return fmt.Errorf("%s must be canonical and absolute", name)
		}
	}
	if !safeEnvironmentValue(config.User) || !safeEnvironmentValue(config.Logname) {
		return fmt.Errorf("manifested operator identity is invalid")
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

// OSProcessStarter starts direct Tart children without CommandContext so their
// ownership is not coupled to the initiating command's cancellation.
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
