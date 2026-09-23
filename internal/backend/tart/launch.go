package tart

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
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
	scratch, scratchInfo, err := createScratch(request.GenerationDirectory)
	if err != nil {
		return nil, fmt.Errorf("start Tart: %w", err)
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
			"TMPDIR=" + scratch,
			"LANG=C",
			"LC_ALL=C",
		},
		dir: scratch,
	}
	handle, err := l.process.start(ctx, spec)
	if handle == nil {
		if err == nil {
			err = fmt.Errorf("start Tart process: no owned handle returned")
		}
		return nil, errors.Join(err, cleanupScratch(scratch, scratchInfo))
	}
	owned := &scratchHandle{Handle: handle, path: scratch, info: scratchInfo}
	if err != nil {
		// A returned handle remains the sole stop/reap authority even when
		// startup reports an error after creating the process.
		return owned, fmt.Errorf("start Tart process: %w", err)
	}
	return owned, nil
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
	spawn func(string, []string, *os.ProcAttr) (*os.Process, error)
}

func (s osProcessStarter) start(ctx context.Context, spec processSpec) (backend.Handle, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !supportsOwnedProcessGroups() {
		return nil, fmt.Errorf("start Tart process: owned process groups are unsupported on this platform")
	}
	devNull, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("open null standard streams: %w", err)
	}
	defer devNull.Close()
	attr := &os.ProcAttr{
		Dir:   spec.dir,
		Env:   append([]string(nil), spec.env...),
		Files: []*os.File{devNull, devNull, devNull},
		Sys:   ownedProcessGroupAttributes(),
	}
	spawn := s.spawn
	if spawn == nil {
		spawn = os.StartProcess
	}
	process, err := spawn(spec.path, append([]string{spec.path}, spec.args...), attr)
	if err != nil {
		return nil, err
	}
	if process == nil {
		return nil, fmt.Errorf("start Tart process: spawn returned no process")
	}
	return &osProcessHandle{process: process, done: make(chan struct{}), signalGroup: signalOwnedProcessGroup}, nil
}

type osProcessHandle struct {
	process        *os.Process
	done           chan struct{}
	signalGroup    func(int) error
	stopMu         sync.Mutex
	stopSent       bool
	reaped         bool
	authorityLost  bool
	waitOnce       sync.Once
	waitErr        error
	pollWait       func(int) (int, syscall.WaitStatus, error)
	releaseProcess func() error
}

// ErrReapUnproven means the retained direct-child wait no longer proves that
// the fixed Tart process was reaped. Callers must preserve generation state.
var ErrReapUnproven = errors.New("owned Tart process reap is unproven")

func (h *osProcessHandle) Stop(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if h == nil || h.process == nil {
		return fmt.Errorf("owned Tart process is unavailable")
	}
	signalGroup := h.signalGroup
	if signalGroup == nil {
		signalGroup = signalOwnedProcessGroup
	}
	h.stopMu.Lock()
	defer h.stopMu.Unlock()
	if h.reaped {
		return nil
	}
	if h.authorityLost {
		return ErrReapUnproven
	}
	if h.stopSent {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := signalGroup(-h.process.Pid); err != nil {
		return err
	}
	h.stopSent = true
	return nil
}

func (h *osProcessHandle) Wait(ctx context.Context) error {
	if h == nil || h.process == nil {
		return fmt.Errorf("owned Tart process is unavailable")
	}
	select {
	case <-h.done:
		return h.waitErr
	default:
	}
	h.waitOnce.Do(func() { go h.reap() })
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-h.done:
		return h.waitErr
	}
}

// reap and Stop share stopMu. The child stays unreaped while Stop can address
// its process group, so the group ID cannot be recycled before the signal.
func (h *osProcessHandle) reap() {
	poll := h.pollWait
	if poll == nil {
		poll = func(childID int) (int, syscall.WaitStatus, error) {
			var status syscall.WaitStatus
			got, err := syscall.Wait4(childID, &status, syscall.WNOHANG, nil)
			return got, status, err
		}
	}
	for {
		h.stopMu.Lock()
		childID := h.process.Pid
		pid, status, err := poll(childID)
		if errors.Is(err, syscall.EINTR) {
			h.stopMu.Unlock()
			continue
		}
		if pid == 0 && err == nil {
			h.stopMu.Unlock()
			time.Sleep(25 * time.Millisecond)
			continue
		}
		if err != nil || pid != childID {
			if err != nil {
				h.waitErr = fmt.Errorf("%w: wait for process %d: %w", ErrReapUnproven, childID, err)
			} else {
				h.waitErr = fmt.Errorf("%w: wait for process %d returned pid %d", ErrReapUnproven, childID, pid)
			}
			h.authorityLost = true
			close(h.done)
			h.stopMu.Unlock()
			return
		}
		switch {
		case status.Signaled():
			h.waitErr = fmt.Errorf("owned Tart process %d terminated by signal %s", pid, status.Signal())
		case !status.Exited():
			h.waitErr = fmt.Errorf("owned Tart process %d exited with unknown status %#x", childID, status)
		case status.ExitStatus() != 0:
			h.waitErr = fmt.Errorf("owned Tart process %d exited with status %d", childID, status.ExitStatus())
		}
		release := h.releaseProcess
		if release == nil {
			release = h.process.Release
		}
		if releaseErr := release(); releaseErr != nil {
			h.waitErr = errors.Join(h.waitErr, fmt.Errorf("release owned Tart process %d: %w", childID, releaseErr))
		}
		h.reaped = true
		close(h.done)
		h.stopMu.Unlock()
		return
	}
}
