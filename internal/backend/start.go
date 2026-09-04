package backend

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"unicode"
)

// StartRequest identifies one already-created backend object and its
// supervisor-owned runtime endpoints. It deliberately cannot carry backend
// flags or generic host integration options.
type StartRequest struct {
	ObjectID            string
	SerialDevice        string
	GenerationDirectory string
}

// Handle is the exact process lifetime owned by a successful backend start.
// The supervisor explicitly stops and waits for it; it never adopts a PID.
type Handle interface {
	Stop(context.Context) error
	Wait(context.Context) error
}

// Starter launches one existing backend object using a fixed backend policy.
type Starter interface {
	Start(context.Context, StartRequest) (Handle, error)
}

// AddressResolver reads a current management address without making it
// durable identity.
type AddressResolver interface {
	Resolve(context.Context, string) (string, error)
}

// ValidateStartRequest ensures every backend-neutral launch operand is a
// bounded, canonical single value before a backend adapter constructs argv.
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
