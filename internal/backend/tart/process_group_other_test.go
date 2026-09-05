//go:build !darwin

package tart

import (
	"context"
	"os/exec"
	"testing"
)

func TestOSProcessStarterRejectsBeforeSpawnWithoutDarwinProcessGroups(t *testing.T) {
	spawned := false
	starter := osProcessStarter{spawn: func(*exec.Cmd) error {
		spawned = true
		return nil
	}}
	if _, err := starter.start(context.Background(), processSpec{path: "/opt/qualified/tart"}); err == nil {
		t.Fatal("start() error = nil, want unsupported-platform refusal")
	}
	if spawned {
		t.Fatal("start() invoked spawn without an owned process-group implementation")
	}
}
