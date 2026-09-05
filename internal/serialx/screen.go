package serialx

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/weshofmann/boxwarden/internal/hostx"
)

const (
	ScreenPath    = hostx.ScreenPath
	ScreenSHA256  = hostx.ScreenExecutableSHA256
	ScreenVersion = hostx.ScreenVersionOutput
)

// ScreenBinary is the read-only fact already admitted by hostx. It gives this
// package no host inspection or generic process-execution capability.
type ScreenBinary = hostx.ScreenAdmission
type ScreenSpec struct {
	Path  string
	Args  []string
	Stdin *os.File
}

// ScreenEvidence is minted by the exact direct-child starter. Its fields are
// private so callers can observe evidence but cannot fabricate it.
type ScreenEvidence struct {
	pid     int
	started time.Time
	token   [16]byte
}

func (e ScreenEvidence) PID() int             { return e.pid }
func (e ScreenEvidence) StartedAt() time.Time { return e.started }
func (e ScreenEvidence) valid() bool {
	return e.pid > 0 && !e.started.IsZero() && e.token != [16]byte{}
}

// NewScreenEvidence binds a direct starter's observed PID/start time to an
// unguessable local token. Task 4 records and revalidates this evidence with
// its process-start proof; this package never discovers processes by PID.
func NewScreenEvidence(pid int, started time.Time) (ScreenEvidence, error) {
	if pid <= 0 || started.IsZero() {
		return ScreenEvidence{}, errors.New("Screen PID and start time are required")
	}
	var token [16]byte
	if _, err := rand.Read(token[:]); err != nil {
		return ScreenEvidence{}, fmt.Errorf("generate Screen evidence token: %w", err)
	}
	return ScreenEvidence{pid: pid, started: started, token: token}, nil
}

type ScreenChild interface {
	Stop(context.Context) error
	Wait(context.Context) error
	Evidence() ScreenEvidence
}
type ScreenStarter interface {
	StartScreen(context.Context, ScreenSpec) (ScreenChild, error)
}

func StartScreen(ctx context.Context, starter ScreenStarter, binary ScreenBinary, operatorSlave *os.File, sessionName string) (ScreenChild, error) {
	if starter == nil {
		return nil, fmt.Errorf("screen starter is required")
	}
	if !qualifiedScreen(binary) {
		return nil, fmt.Errorf("screen binary is not qualified")
	}
	if operatorSlave == nil || sessionName == "" {
		return nil, fmt.Errorf("operator slave and screen session name are required")
	}
	child, err := starter.StartScreen(ctx, ScreenSpec{Path: ScreenPath, Args: []string{"-D", "-m", "-S", sessionName}, Stdin: operatorSlave})
	if err != nil {
		return nil, err
	}
	if child == nil || !child.Evidence().valid() {
		return nil, errors.New("Screen child or direct-child evidence is invalid")
	}
	return child, nil
}
func qualifiedScreen(b ScreenBinary) bool {
	return b.ValidForRuntime()
}
