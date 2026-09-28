package app

import (
	"bytes"
	"strings"
	"testing"

	"github.com/weshofmann/boxwarden/internal/config"
)

func TestWorkspaceReconcileRequiresExternalStorageBeforeBackendConstruction(t *testing.T) {
	path, _ := writeV2DomainFixture(t, "alpha")
	args := []string{"--config", path, "--domain", "alpha", "workspace", "reconcile", "00112233-4455-4677-8899-aabbccddeeff"}
	parsed, err := parseCommand(args, Options{})
	if err != nil || parsed.kind != commandWorkspaceReconcile || !parsed.requiresWorkspaceStorage() || !parsed.requiresBackend() {
		t.Fatalf("reconcile parser missed guarded backend path: %+v, %v", parsed, err)
	}
	constructed := false
	options := Options{Output: &bytes.Buffer{}, BackendFactory: func(config.Config, config.Domain) (BackendDependencies, error) {
		constructed = true
		return BackendDependencies{}, nil
	}}
	if err := Run(t.Context(), args, options); err == nil || !strings.Contains(err.Error(), "workspace storage") || constructed {
		t.Fatalf("unenrolled reconcile reached backend: built=%v, err=%v", constructed, err)
	}
}
