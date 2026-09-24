package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/weshofmann/boxwarden/internal/domain"
)

const (
	probeVolumeID = "00112233-4455-4677-8899-aabbccddeeff"
	probeFSUUID   = "10213243-5465-4768-899a-bbccddeeff00"
)

func TestAdmitPlannedOwnershipRequiresExactPrivateNewVolume(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(t.TempDir(), "ownership.json")
	wantPath := filepath.Join(root, "volumes", probeVolumeID+".raw")
	entry := map[string]any{"domain": "alpha", "volume_id": probeVolumeID, "filesystem_uuid": probeFSUUID,
		"path": wantPath, "size_bytes": 64 << 20, "state": "planned"}
	write := func(entries []map[string]any) {
		t.Helper()
		contents, err := json.Marshal(map[string]any{"version": 1, "alpha_state": filepath.Dir(root), "owned_volumes": entries})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(manifestPath, contents, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write([]map[string]any{entry})
	if err := admitPlannedOwnership(manifestPath, root, domain.ID("alpha"), probeVolumeID, probeFSUUID); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name    string
		entries []map[string]any
	}{
		{"missing", nil},
		{"duplicate", []map[string]any{entry, entry}},
		{"wrong-path", []map[string]any{{"domain": "alpha", "volume_id": probeVolumeID,
			"filesystem_uuid": probeFSUUID, "path": filepath.Join(root, "other.raw"), "size_bytes": 64 << 20, "state": "planned"}}},
		{"already-used", []map[string]any{{"domain": "alpha", "volume_id": probeVolumeID,
			"filesystem_uuid": probeFSUUID, "path": wantPath, "size_bytes": 64 << 20, "state": "qualified"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			write(tc.entries)
			if err := admitPlannedOwnership(manifestPath, root, domain.ID("alpha"), probeVolumeID, probeFSUUID); err == nil {
				t.Fatal("unplanned or conflicting volume admitted")
			}
		})
	}
	if err := os.WriteFile(manifestPath, []byte(`{"version":1,"alpha_state":"x","owned_volumes":[],"owned_volumes":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := admitPlannedOwnership(manifestPath, root, domain.ID("alpha"), probeVolumeID, probeFSUUID); err == nil {
		t.Fatal("duplicate ownership key accepted")
	}
}
