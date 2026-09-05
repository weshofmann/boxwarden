package serialx

import (
	"context"
	"fmt"

	"github.com/weshofmann/boxwarden/internal/hostx"
)

const (
	ScreenPath    = hostx.ScreenPath
	ScreenSHA256  = hostx.ScreenExecutableSHA256
	ScreenVersion = hostx.ScreenVersionOutput
)

// ScreenBinary is the read-only fact already admitted by hostx. It gives this
// package no host inspection or generic process-execution capability.
type ScreenBinary = hostx.ScreenFact
type ScreenSpec struct {
	Path  string
	Args  []string
	Stdin string
}
type ScreenChild interface{ Wait() error }
type ScreenStarter interface {
	StartScreen(context.Context, ScreenSpec) (ScreenChild, error)
}

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
	return starter.StartScreen(ctx, ScreenSpec{Path: ScreenPath, Args: []string{"-D", "-m", "-S", sessionName}, Stdin: operatorSlave})
}
func qualifiedScreen(b ScreenBinary) bool {
	return b.Qualified()
}
