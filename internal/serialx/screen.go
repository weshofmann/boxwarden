package serialx

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/weshofmann/boxwarden/internal/hostx"
)

const (
	ScreenPath    = hostx.ScreenPath
	ScreenSHA256  = hostx.ScreenExecutableSHA256
	ScreenVersion = hostx.ScreenVersionOutput
)

type ScreenBinary = hostx.ScreenAdmission

// ScreenEvidence is minted solely from a kernel observation of the direct
// child returned by exec.Cmd.Start. Its fields stay private so a caller cannot
// turn a PID into process-control authority.
type ScreenEvidence struct {
	pid     int
	started time.Time
	unique  uint64
}

var (
	// ErrScreenObservation means the trusted host could not reobserve the
	// retained direct child; it is different from a normally reaped exit.
	ErrScreenObservation = errors.New("direct Screen child observation failed")
	// ErrScreenIdentityMismatch means the PID no longer has the start/unique
	// identity captured immediately after exec.Cmd.Start.
	ErrScreenIdentityMismatch = errors.New("direct Screen child identity changed")
)

func (e ScreenEvidence) PID() int             { return e.pid }
func (e ScreenEvidence) StartedAt() time.Time { return e.started }
func (e ScreenEvidence) valid() bool {
	return e.pid > 0 && !e.started.IsZero() && e.unique != 0
}

type screenLaunch struct {
	path  string
	args  []string
	stdin *os.File
}

type screenIdentity struct {
	pid     int
	started time.Time
	unique  uint64
}

func (i screenIdentity) evidence() ScreenEvidence {
	return ScreenEvidence{pid: i.pid, started: i.started, unique: i.unique}
}
func (i screenIdentity) valid() bool { return i.evidence().valid() }
func (i screenIdentity) matches(e ScreenEvidence) bool {
	return i.pid == e.pid && i.started.Equal(e.started) && i.unique == e.unique
}

// screenCommand is always the direct child created in this package. It is
// private: other packages cannot supply an adopted PID or replacement target.
type screenCommand interface {
	PID() int
	Signal() error
	Kill() error
	Wait() error
}

type startedScreen struct {
	cmd    *exec.Cmd // retained in production for direct-child ownership
	direct screenCommand
}

type screenStartDeps struct {
	start   func(context.Context, screenLaunch) (startedScreen, error)
	observe func(context.Context, int) (screenIdentity, error)
}

// ownedScreen owns one exact direct child. Wait is cached so the lifecycle
// watcher and shutdown path cannot race to reap it.
type ownedScreen struct {
	cmd      *exec.Cmd
	direct   screenCommand
	evidence ScreenEvidence
	observe  func(context.Context, int) (screenIdentity, error)
	waitOnce sync.Once
	waitDone chan struct{}
	waitErr  error
}

func startOwnedScreen(ctx context.Context, launch screenLaunch, deps screenStartDeps) (*ownedScreen, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if deps.start == nil || deps.observe == nil {
		return nil, errors.New("Screen production dependencies are unavailable")
	}
	started, err := deps.start(ctx, launch)
	if err != nil {
		return nil, err
	}
	if started.direct == nil || started.direct.PID() <= 0 {
		return nil, errors.New("Screen start returned no direct child")
	}
	identity, err := deps.observe(ctx, started.direct.PID())
	if err != nil || !identity.valid() || identity.pid != started.direct.PID() {
		cleanupUnobservedScreen(started.direct)
		if err != nil {
			return nil, fmt.Errorf("observe direct Screen child: %w", err)
		}
		return nil, errors.New("direct Screen child kernel identity is invalid")
	}
	return newOwnedScreen(started, identity, deps.observe), nil
}

func newOwnedScreen(started startedScreen, identity screenIdentity, observe func(context.Context, int) (screenIdentity, error)) *ownedScreen {
	return &ownedScreen{cmd: started.cmd, direct: started.direct, evidence: identity.evidence(), observe: observe, waitDone: make(chan struct{})}
}

func cleanupUnobservedScreen(direct screenCommand) {
	// Start succeeded, so this is still the held direct child rather than a PID
	// reconstructed from a process table. This cleanup is intentionally local.
	_ = direct.Kill()
	done := make(chan struct{})
	go func() { _ = direct.Wait(); close(done) }()
	// An uninterruptible child must not wedge the failed admission path. SIGKILL
	// has already been sent through the held direct-child handle; a later reap
	// is harmless, while normal Screen lifecycle still retains and waits exactly
	// once after successful observation.
	select {
	case <-done:
	case <-time.After(ExchangeDeadline):
	}
}

func (s *ownedScreen) Evidence() ScreenEvidence {
	if s == nil {
		return ScreenEvidence{}
	}
	return s.evidence
}

func (s *ownedScreen) Check(ctx context.Context) error {
	if s == nil || s.direct == nil || !s.evidence.valid() || s.observe == nil {
		return errors.New("direct Screen child evidence is unavailable")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	identity, err := s.observe(ctx, s.evidence.pid)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrScreenObservation, err)
	}
	if !identity.matches(s.evidence) {
		return ErrScreenIdentityMismatch
	}
	return nil
}

func (s *ownedScreen) Stop(ctx context.Context) error {
	// Wait is the only reaping path. Once it has completed, the direct child
	// handle is historical evidence only: signaling through its PID could reach
	// an unrelated process after PID reuse.
	select {
	case <-s.waitDone:
		return nil
	default:
	}
	if err := s.Check(ctx); err != nil {
		return err
	}
	return s.direct.Signal()
}

func (s *ownedScreen) Wait(ctx context.Context) error {
	if s == nil || s.direct == nil {
		return errors.New("direct Screen child is unavailable")
	}
	s.waitOnce.Do(func() {
		go func() {
			s.waitErr = s.direct.Wait()
			close(s.waitDone)
		}()
	})
	select {
	case <-s.waitDone:
		return s.waitErr
	case <-ctx.Done():
		return ctx.Err()
	}
}

func qualifiedScreen(b ScreenBinary) bool { return b.ValidForRuntime() }
