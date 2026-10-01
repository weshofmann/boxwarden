//go:build n1diagnostic && !n1candidate && (darwin || linux)

package tart

import (
	"context"
	"errors"
	"fmt"
	"github.com/weshofmann/boxwarden/internal/backend"
	"github.com/weshofmann/boxwarden/internal/networkdiag"
	"os"
	"path/filepath"
	"sync"
	"syscall"
)

var ErrDiagnosticCleanupUnproven = errors.New("diagnostic transport cleanup unproven")

type DiagnosticLaunchBinding struct {
	Generation, Nonce string
	CandidateMAC      [6]uint8
}
type DiagnosticLauncher struct {
	config  LaunchConfig
	binding DiagnosticLaunchBinding
	spawn   func(context.Context, processSpec, *os.File) (backend.Handle, error)
}

func NewDiagnosticLauncher(c LaunchConfig, b DiagnosticLaunchBinding) DiagnosticLauncher {
	return DiagnosticLauncher{config: c, binding: b}
}
func (l DiagnosticLauncher) Start(ctx context.Context, r backend.StartRequest) (backend.Handle, error) {
	if !networkdiag.UUID(l.binding.Generation) || !networkdiag.UUID(l.binding.Nonce) || !networkdiag.MAC(l.binding.CandidateMAC) || filepath.Base(r.GenerationDirectory) != l.binding.Generation {
		return nil, networkdiag.ErrMetadata
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := backend.ValidateStartRequest(r); err != nil {
		return nil, err
	}
	if err := validateLaunchConfig(l.config); err != nil {
		return nil, err
	}
	p := &diagnosticProcessStarter{binding: l.binding, spawn: l.spawn}
	h, err := newLauncher(l.config, p).Start(ctx, r)
	if h == nil {
		if err != nil && (!p.entered || r.ManagedDisks != nil) {
			err = errors.Join(err, ErrDiagnosticCleanupUnproven)
		}
		return nil, err
	}
	return &diagnosticHandle{Handle: h, watch: p.watch, managed: r.ManagedDisks != nil, child: p.child}, err
}

type diagnosticProcessStarter struct {
	binding DiagnosticLaunchBinding
	spawn   func(context.Context, processSpec, *os.File) (backend.Handle, error)
	watch   *networkdiag.Watch
	entered bool
	child   networkdiag.ProcessCorrelation
}

func (p *diagnosticProcessStarter) start(ctx context.Context, spec processSpec) (h backend.Handle, result error) {
	p.entered = true
	if len(spec.args) < 2 || spec.args[0] != "run" || spec.args[1] != "--net-softnet" {
		return nil, networkdiag.ErrMetadata
	}
	selector := "--net-softnet-block=@boxwarden-n1-diagnostic:" + p.binding.Generation + ":" + p.binding.Nonce
	spec.args = append([]string{spec.args[0], spec.args[1], selector}, spec.args[2:]...)
	parent, child, err := diagnosticSocketpair()
	if err != nil {
		return nil, err
	}
	defer func() {
		if e := child.Close(); e != nil {
			result = errors.Join(result, fmt.Errorf("%w: child endpoint close: %w", ErrDiagnosticCleanupUnproven, e))
		}
	}()
	spawn := p.spawn
	if spawn == nil {
		spawn = startDiagnosticProcess
	}
	clock, clockErr := networkdiag.NewLaunchClock()
	if clockErr != nil {
		return nil, errors.Join(clockErr, parent.Close())
	}
	h, err = spawn(ctx, spec, child)
	if h == nil {
		if e := parent.Close(); e != nil {
			err = errors.Join(err, fmt.Errorf("%w: parent endpoint close: %w", ErrDiagnosticCleanupUnproven, e))
		}
		return nil, err
	}
	p.watch = networkdiag.NewWatchAt(parent, p.binding.Generation, p.binding.Nonce, p.binding.CandidateMAC, clock)
	p.child, clockErr = forwardedDiagnosticProcess(h)
	if clockErr != nil {
		p.watch.Invalidate()
		return h, errors.Join(err, clockErr)
	}
	if err != nil {
		return h, err
	}
	_, err = p.watch.AwaitHello(ctx)
	return h, err
}
func diagnosticSocketpair() (*os.File, *os.File, error) {
	syscall.ForkLock.RLock()
	fds, err := syscall.Socketpair(syscall.AF_UNIX, syscall.SOCK_STREAM, 0)
	if err != nil {
		syscall.ForkLock.RUnlock()
		return nil, nil, networkdiag.ErrMetadata
	}
	for _, fd := range fds {
		_, _, e := syscall.Syscall(syscall.SYS_FCNTL, uintptr(fd), syscall.F_SETFD, syscall.FD_CLOEXEC)
		if e != 0 {
			cleanup := closeDiagnosticFDs(fds)
			syscall.ForkLock.RUnlock()
			return nil, nil, errors.Join(networkdiag.ErrMetadata, e, cleanup)
		}
	}
	syscall.ForkLock.RUnlock()
	for _, fd := range fds {
		if e := syscall.SetNonblock(fd, true); e != nil {
			return nil, nil, errors.Join(networkdiag.ErrMetadata, e, closeDiagnosticFDs(fds))
		}
	}
	return os.NewFile(uintptr(fds[0]), "diagnostic-watch"), os.NewFile(uintptr(fds[1]), "diagnostic-child-stdout"), nil
}
func closeDiagnosticFDs(fds [2]int) error {
	var result error
	for _, fd := range fds {
		if err := syscall.Close(fd); err != nil {
			result = errors.Join(result, fmt.Errorf("%w: socketpair close: %w", ErrDiagnosticCleanupUnproven, err))
		}
	}
	return result
}
func startDiagnosticProcess(ctx context.Context, spec processSpec, child *os.File) (h backend.Handle, result error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !supportsOwnedProcessGroups() {
		return nil, networkdiag.ErrMetadata
	}
	null, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := null.Close(); err != nil {
			result = errors.Join(result, fmt.Errorf("%w: null close: %w", ErrDiagnosticCleanupUnproven, err))
		}
	}()
	process, err := os.StartProcess(spec.path, append([]string{spec.path}, spec.args...), &os.ProcAttr{Dir: spec.dir, Env: append([]string(nil), spec.env...), Files: []*os.File{null, child, null}, Sys: ownedProcessGroupAttributes()})
	if err != nil {
		return nil, err
	}
	if process == nil {
		return nil, networkdiag.ErrMetadata
	}
	return &osProcessHandle{process: process, done: make(chan struct{}), signalGroup: signalOwnedProcessGroup}, nil
}

type diagnosticHandle struct {
	backend.Handle
	watch      *networkdiag.Watch
	once       sync.Once
	cleanupErr error
	managed    bool
	child      networkdiag.ProcessCorrelation
}

func (h *diagnosticHandle) DiagnosticWatch() *networkdiag.Watch { return h.watch }
func (h *diagnosticHandle) RetainedChildLive() bool {
	l, ok := h.Handle.(backend.RetainedChildLiveness)
	return ok && l.RetainedChildLive()
}
func (h *diagnosticHandle) RequestStop(ctx context.Context) error {
	r, ok := h.Handle.(interface{ RequestStop(context.Context) error })
	if !ok {
		return networkdiag.ErrMetadata
	}
	return r.RequestStop(ctx)
}
func (h *diagnosticHandle) Wait(ctx context.Context) error {
	err := h.Handle.Wait(ctx)
	if ctx.Err() != nil {
		return errors.Join(err, ctx.Err())
	}
	if errors.Is(err, ErrReapUnproven) {
		return err
	}
	if err != nil && h.managed {
		err = errors.Join(err, ErrDiagnosticCleanupUnproven)
	}
	h.once.Do(func() {
		if h.watch != nil {
			if e := h.watch.Close(); e != nil {
				h.cleanupErr = fmt.Errorf("%w: watch close: %w", ErrDiagnosticCleanupUnproven, e)
			}
		}
	})
	return errors.Join(err, h.cleanupErr)
}

func (h *diagnosticHandle) RetainedDiagnosticProcess() (networkdiag.ProcessCorrelation, error) {
	if h == nil || !h.child.Valid() || !h.RetainedChildLive() {
		return networkdiag.ProcessCorrelation{}, networkdiag.ErrMetadata
	}
	p, e := forwardedDiagnosticProcess(h.Handle)
	if e != nil || p != h.child || !h.RetainedChildLive() {
		return networkdiag.ProcessCorrelation{}, networkdiag.ErrMetadata
	}
	return p, nil
}
