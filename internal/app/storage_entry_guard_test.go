package app

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/weshofmann/boxwarden/internal/config"
	"github.com/weshofmann/boxwarden/internal/hostidentity"
)

func enrolledEntryFixture(t *testing.T) (string, config.Domain) {
	t.Helper()
	source, selected := writeV2DomainFixture(t, "alpha")
	loaded, err := config.Load(source)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := loaded.EnrolledCopy("alpha", config.WorkspaceStorage{MountPoint: selected.StateRoot,
		VolumeUUID: "00112233-4455-6677-8899-aabbccddeeff"})
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "enrolled.json")
	if err := os.WriteFile(target, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return target, selected
}

func TestEnrolledDomainStateEntryChecksStorageBeforeLocksAndFactories(t *testing.T) {
	configPath, selected := enrolledEntryFixture(t)
	prepare := []string{"alpha", "prepare", "--recipe", "/tmp/recipe.json", "--iso", "/tmp/ubuntu.iso",
		"--guest-definition", "/tmp/guest", "--openssl", "/usr/bin/openssl", "--openssl-sha256", strings.Repeat("a", 64),
		"--xorriso", "/usr/bin/xorriso", "--xorriso-sha256", strings.Repeat("b", 64)}
	commands := []struct {
		name string
		args []string
	}{
		{"domain init", []string{"domain", "init"}},
		{"golden register", []string{"golden", "register", "bw-alpha-base"}},
		{"session create", []string{"session", "create", "dev"}},
		{"session status", []string{"session", "status", "dev"}},
		{"session action list", []string{"session", "action", "list", "dev"}},
		{"session action run", []string{"session", "action", "run", "once", "setup", "dev"}},
		{"clipboard push", []string{"clipboard", "push", "dev"}},
		{"alpha prepare", prepare},
	}
	for _, command := range commands {
		t.Run(command.name, func(t *testing.T) {
			guardErr := errors.New("injected wrong backing volume")
			checked, constructed := 0, 0
			options := Options{Output: &bytes.Buffer{}, storageCheck: func(hostidentity.StorageExpectation) error {
				checked++
				return guardErr
			}, BackendFactory: func(config.Config, config.Domain) (BackendDependencies, error) {
				constructed++
				return BackendDependencies{}, errors.New("backend factory must remain unreachable")
			}}
			args := append([]string{"--config", configPath, "--domain", "alpha"}, command.args...)
			if err := Run(t.Context(), args, options); !errors.Is(err, guardErr) || checked != 1 || constructed != 0 {
				t.Fatalf("storage guard not first: checked=%d backend=%d err=%v", checked, constructed, err)
			}
			if _, err := os.Lstat(filepath.Join(selected.StateRoot, "locks")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("guarded command created lock directory: %v", err)
			}
		})
	}
}

func TestUnenrolledLegacySessionCreateCanReachBackendFactory(t *testing.T) {
	configPath, _ := writeV2DomainFixture(t, "alpha")
	constructErr := errors.New("synthetic backend factory")
	checked, constructed := 0, 0
	options := Options{Output: &bytes.Buffer{}, storageCheck: func(hostidentity.StorageExpectation) error {
		checked++
		return errors.New("legacy config should not be checked for session create")
	}, BackendFactory: func(config.Config, config.Domain) (BackendDependencies, error) {
		constructed++
		return BackendDependencies{}, constructErr
	}}
	err := Run(t.Context(), []string{"--config", configPath, "--domain", "alpha", "session", "create", "dev"}, options)
	if !errors.Is(err, constructErr) || checked != 0 || constructed != 1 {
		t.Fatalf("legacy session create changed: checked=%d backend=%d err=%v", checked, constructed, err)
	}
}

func TestEnrolledStorageFailureDoesNotBlockStopContainmentFactory(t *testing.T) {
	configPath, _ := enrolledEntryFixture(t)
	constructErr := errors.New("synthetic stopper factory")
	checked, constructed := 0, 0
	options := Options{Output: &bytes.Buffer{}, storageCheck: func(hostidentity.StorageExpectation) error {
		checked++
		return errors.New("backing storage unavailable")
	}, SessionStopperFactory: func(config.Config, config.Domain, string) (SessionStopper, error) {
		constructed++
		return nil, constructErr
	}}
	err := Run(t.Context(), []string{"--config", configPath, "--domain", "alpha", "session", "stop", "dev"}, options)
	if !errors.Is(err, constructErr) || checked != 0 || constructed != 1 {
		t.Fatalf("stop containment was blocked: checked=%d factory=%d err=%v", checked, constructed, err)
	}
}
