package serialx

import (
	"context"
	"fmt"
)

const (
	ScreenPath    = "/usr/bin/screen"
	ScreenSHA256  = "07b706b76c0e7374eb524f9e2e738437f208b4b123d7d9b7b2666019c8881add"
	ScreenVersion = "Screen version 4.00.03 (FAU) 23-Oct-06"
)

// ScreenBinary is the already-inspected immutable executable fact. This
// package cannot inspect arbitrary host paths or execute a shell.
type ScreenBinary struct {
	Path, SHA256, Version string
	Mode                  uint32
	UID, GID              int
	Links                 uint64
}

type ScreenSpec struct {
	Path  string
	Args  []string
	Stdin string
}

// ScreenChild is deliberately wait-only. It provides no Screen control/data
// surface such as logs, hardcopy, stuff, paste, or control commands.
type ScreenChild interface {
	Wait() error
}

type ScreenStarter interface {
	StartScreen(context.Context, ScreenSpec) (ScreenChild, error)
}

// StartScreen starts the retained direct child on the exact operator slave.
// The caller owns process lifetime; only this narrow method chooses Screen's
// fixed path and argv.
func StartScreen(ctx context.Context, starter ScreenStarter, binary ScreenBinary, operatorSlave, sessionName string) (ScreenChild, error) {
	if starter == nil {
		return nil, fmt.Errorf("screen starter is required")
	}
	if !qualifiedScreen(binary) {
		return nil, fmt.Errorf("screen binary is not qualified")
	}
	if operatorSlave == "" || sessionName == "" {
		return nil, fmt.Errorf("operator slave and screen session name are required")
	}
	return starter.StartScreen(ctx, ScreenSpec{
		Path:  ScreenPath,
		Args:  []string{"-D", "-m", "-S", sessionName},
		Stdin: operatorSlave,
	})
}

func qualifiedScreen(binary ScreenBinary) bool {
	return binary.Path == ScreenPath && binary.Mode == 0o755 && binary.UID == 0 && binary.GID == 0 && binary.Links == 1 && binary.SHA256 == ScreenSHA256 && binary.Version == ScreenVersion
}
