package backend

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"unicode"
)

// StartRequest identifies one existing backend object and its exact
// supervisor-owned runtime paths. It intentionally contains no backend flags.
type StartRequest struct {
	ObjectID            string
	SerialDevice        string
	GenerationDirectory string
}

// Handle owns the exact process lifetime created by a successful start.
// Callers can neither supply nor reconstruct a process identifier.
type Handle interface {
	Stop(context.Context) error
	Wait(context.Context) error
}

// Starter launches one existing backend object with its fixed adapter policy.
type Starter interface {
	Start(context.Context, StartRequest) (Handle, error)
}

// AddressResolver returns a fresh management address. Addresses are runtime
// observations, never durable backend identity.
type AddressResolver interface {
	Resolve(context.Context, string) (string, error)
}

// ValidateStartRequest admits an exact object and two canonical, private
// supervisor paths before an adapter uses them as direct argv operands.
func ValidateStartRequest(request StartRequest) error {
	if err := ValidateObjectID(request.ObjectID); err != nil {
		return fmt.Errorf("invalid backend object ID: %w", err)
	}
	if !canonicalAbsolutePath(request.SerialDevice) {
		return fmt.Errorf("serial device must be canonical and absolute")
	}
	if !canonicalAbsolutePath(request.GenerationDirectory) {
		return fmt.Errorf("generation directory must be canonical and absolute")
	}
	return nil
}

func canonicalAbsolutePath(path string) bool {
	return path != "/" && filepath.IsAbs(path) && filepath.Clean(path) == path && strings.IndexFunc(path, unicode.IsControl) < 0
}
