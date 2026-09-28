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

func TestWorkspaceStorageEnrollWritesNewConfigWithNormalizedOperatorUUID(t *testing.T) {
	source, selected := writeV2DomainFixture(t, "alpha")
	target := filepath.Join(t.TempDir(), "enrolled.json")
	const upper = "00112233-4455-6677-8899-AABBCCDDEEFF"
	const lower = "00112233-4455-6677-8899-aabbccddeeff"
	called := 0
	var output bytes.Buffer
	options := Options{Output: &output, storageEnroll: func(expected hostidentity.StorageExpectation, raw []byte) error {
		called++
		if expected.ConfigPath != target || expected.StateRoot != selected.StateRoot || expected.MountPoint != selected.StateRoot || expected.VolumeUUID != lower {
			t.Fatalf("wrong enrollment binding: %+v", expected)
		}
		return os.WriteFile(target, raw, 0o600)
	}}
	args := []string{"--config", source, "--domain", "alpha", "workspace", "storage", "enroll",
		"--expected-apfs-volume-uuid", upper, "--mount-point", selected.StateRoot, "--output-config", target}
	if err := Run(t.Context(), args, options); err != nil || called != 1 {
		t.Fatalf("enroll = calls %d, output %q, err %v", called, output.String(), err)
	}
	enrolled, err := config.Load(target)
	if err != nil {
		t.Fatal(err)
	}
	actual, err := enrolled.Domain("alpha")
	if err != nil || actual.WorkspaceStorage == nil || actual.WorkspaceStorage.VolumeUUID != lower {
		t.Fatalf("enrolled config = %+v, %v", actual, err)
	}
	if !strings.Contains(output.String(), "storage: enrolled\n") {
		t.Fatalf("enrollment output = %q", output.String())
	}
	if err := Run(t.Context(), args, options); err == nil || called != 1 {
		t.Fatalf("existing output config was overwritten: calls=%d, err=%v", called, err)
	}
	bad := append([]string(nil), args...)
	bad[8] = "not-a-uuid"
	if err := Run(t.Context(), bad, options); err == nil || called != 1 {
		t.Fatalf("malformed operator UUID reached enrollment writer: calls=%d, err=%v", called, err)
	}
}

func TestWorkspaceStorageEnrollReportsPublishedFileAfterSyncFailure(t *testing.T) {
	source, selected := writeV2DomainFixture(t, "alpha")
	target := filepath.Join(t.TempDir(), "possibly-published.json")
	called := 0
	options := Options{Output: &bytes.Buffer{}, storageEnroll: func(expected hostidentity.StorageExpectation, raw []byte) error {
		called++
		if expected.ConfigPath != target {
			t.Fatalf("wrong target: %+v", expected)
		}
		if err := os.WriteFile(target, raw, 0o600); err != nil {
			return err
		}
		return errors.New("injected post-link sync failure")
	}}
	args := []string{"--config", source, "--domain", "alpha", "workspace", "storage", "enroll",
		"--expected-apfs-volume-uuid", "00112233-4455-6677-8899-aabbccddeeff", "--mount-point", selected.StateRoot,
		"--output-config", target}
	if err := Run(t.Context(), args, options); err == nil || !strings.Contains(err.Error(), "must be inspected") {
		t.Fatalf("ambiguous config publication lacked actionable error: %v", err)
	}
	if _, err := os.Lstat(target); err != nil {
		t.Fatalf("published file disappeared after ambiguous error: %v", err)
	}
	if err := Run(t.Context(), args, options); err == nil || called != 1 {
		t.Fatalf("ambiguous config was overwritten: calls=%d, err=%v", called, err)
	}
}
