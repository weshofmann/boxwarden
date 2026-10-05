package config

import (
	"encoding/json"
	"testing"
)

func TestNewEnrolledAlphaConfigProducesExactFreshDeclaration(t *testing.T) {
	host := Host{TartExecutable: "/operator/tart", TartHome: "/operator/tart-home", SoftnetSource: "/Library/softnet"}
	raw, err := NewEnrolledAlphaConfig(host, "/Volumes/private/setup/alpha", WorkspaceStorage{MountPoint: "/Volumes/private", VolumeUUID: "00112233-4455-6677-8899-aabbccddeeff"})
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatal(err)
	}
	if value["version"] != float64(2) {
		t.Fatalf("version = %v", value)
	}
	domains := value["domains"].(map[string]any)
	if len(domains) != 1 || domains["alpha"] == nil {
		t.Fatalf("domains = %v", domains)
	}
	alpha := domains["alpha"].(map[string]any)
	if alpha["state_root"] != "/Volumes/private/setup/alpha" {
		t.Fatalf("alpha = %v", alpha)
	}
	storage := alpha["workspace_storage"].(map[string]any)
	if storage["apfs_volume_uuid"] != "00112233-4455-6677-8899-aabbccddeeff" {
		t.Fatalf("storage = %v", storage)
	}
}

func TestNewEnrolledAlphaConfigRejectsInvalidAndOverlappingPaths(t *testing.T) {
	host := Host{TartExecutable: "/operator/tart", TartHome: "/operator/tart-home", SoftnetSource: "/Library/softnet"}
	storage := WorkspaceStorage{MountPoint: "/Volumes/private", VolumeUUID: "00112233-4455-6677-8899-aabbccddeeff"}
	for _, root := range []string{"relative", "/elsewhere", "/Volumes/private/../private/alpha"} {
		if _, err := NewEnrolledAlphaConfig(host, root, storage); err == nil {
			t.Fatalf("accepted %q", root)
		}
	}
	host.TartHome = "/Volumes/private/setup"
	if _, err := NewEnrolledAlphaConfig(host, "/Volumes/private/setup/alpha", storage); err == nil {
		t.Fatal("accepted host overlap")
	}
}
