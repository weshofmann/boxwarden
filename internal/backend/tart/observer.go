// Package tart observes Tart VM state. It intentionally exposes no lifecycle
// mutations: V0.1 uses Tart only as an observed source of truth.
package tart

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/weshofmann/boxwarden/internal/backend"
	"github.com/weshofmann/boxwarden/internal/execx"
)

// Observer observes Tart objects through its supported JSON list interface.
type Observer struct {
	runner     execx.Runner
	executable string
}

// New constructs an observer using executable. A caller may pass an absolute
// executable path after resolving it on the trusted host.
func New(runner execx.Runner, executable string) Observer {
	return Observer{runner: runner, executable: executable}
}

// Observe reports the named Tart object's observed state without mutating it.
func (o Observer) Observe(ctx context.Context, objectID string) (backend.Observation, error) {
	if o.runner == nil {
		return backend.Observation{}, fmt.Errorf("observe Tart object: runner is required")
	}
	if strings.TrimSpace(o.executable) == "" {
		return backend.Observation{}, fmt.Errorf("observe Tart object: executable is required")
	}

	result, err := o.runner.Run(ctx, execx.Command{
		Path: o.executable,
		Args: []string{"list", "--format", "json"},
	})
	if err != nil {
		return backend.Observation{}, fmt.Errorf("observe Tart object with tart list --format json: %w", err)
	}
	if result.Truncated {
		return backend.Observation{}, fmt.Errorf("observe Tart object: tart list --format json output exceeded the trusted-host limit")
	}

	observation, err := parseList([]byte(result.Stdout), objectID)
	if err != nil {
		return backend.Observation{}, fmt.Errorf("observe Tart object: invalid tart list --format json output: %w", err)
	}
	return observation, nil
}

type listEntry struct {
	State    string
	Accessed string
	Name     string
	Source   string
	Disk     *int64
	Running  *bool
	Size     *int64
}

func parseList(raw []byte, objectID string) (backend.Observation, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()

	var entries []listEntry
	if err := decoder.Decode(&entries); err != nil {
		return backend.Observation{}, fmt.Errorf("decode list: %w", err)
	}
	if err := rejectTrailingJSON(decoder); err != nil {
		return backend.Observation{}, err
	}

	var matches []listEntry
	for _, entry := range entries {
		if err := validateEntry(entry); err != nil {
			return backend.Observation{}, err
		}
		if entry.Name != objectID {
			continue
		}
		matches = append(matches, entry)
	}

	switch len(matches) {
	case 0:
		return backend.Observation{ObjectID: objectID, State: backend.ObjectUnknown}, nil
	case 1:
		return backend.Observation{
			ObjectID: objectID,
			Exists:   true,
			State:    stateFor(matches[0]),
		}, nil
	default:
		return backend.Observation{}, fmt.Errorf("object %q appears %d times", objectID, len(matches))
	}
}

func validateEntry(entry listEntry) error {
	if entry.Name == "" {
		return fmt.Errorf("object name is missing")
	}
	if entry.Accessed == "" {
		return fmt.Errorf("object %q is missing Accessed", entry.Name)
	}
	if _, err := time.Parse(time.RFC3339, entry.Accessed); err != nil {
		return fmt.Errorf("object %q has invalid Accessed time: %w", entry.Name, err)
	}
	if entry.Source == "" {
		return fmt.Errorf("object %q is missing Source", entry.Name)
	}
	if entry.Disk == nil || entry.Size == nil || entry.Running == nil {
		return fmt.Errorf("object %q has incomplete Tart list fields", entry.Name)
	}
	if *entry.Disk < 0 || *entry.Size < 0 {
		return fmt.Errorf("object %q has negative size fields", entry.Name)
	}
	if entry.State != "running" && entry.State != "stopped" {
		return fmt.Errorf("object %q has unsupported state %q", entry.Name, entry.State)
	}
	if (entry.State == "running") != *entry.Running {
		return fmt.Errorf("object %q has inconsistent State and Running fields", entry.Name)
	}
	return nil
}

func stateFor(entry listEntry) backend.ObjectState {
	if *entry.Running {
		return backend.ObjectRunning
	}
	return backend.ObjectStopped
}

func rejectTrailingJSON(decoder *json.Decoder) error {
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return fmt.Errorf("multiple JSON values")
		}
		return fmt.Errorf("decode trailing data: %w", err)
	}
	return nil
}
