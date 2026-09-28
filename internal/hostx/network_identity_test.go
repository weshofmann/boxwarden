package hostx

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCandidatePackageMatchesClosedAdmission(t *testing.T) {
	data, err := os.ReadFile("../../tools/n1-softnet/artifact.json")
	if err != nil {
		t.Fatal(err)
	}
	var artifact struct {
		Version    string `json:"version"`
		Executable string `json:"executable_sha256"`
		Archive    string `json:"archive_sha256"`
	}
	if err := json.Unmarshal(data, &artifact); err != nil {
		t.Fatal(err)
	}
	if artifact.Version != "0.19.0-boxwarden-n1.1" || len(artifact.Executable) != 64 || len(artifact.Archive) != 64 {
		t.Fatal("candidate package lacks a complete, distinct artifact identity")
	}
	candidate := ToolIdentity{
		Path:    filepath.Join("/Library/Boxwarden/toolchains/softnet", artifact.Version, artifact.Executable, "softnet"),
		Version: artifact.Version, ExecutableSHA256: artifact.Executable, ArchiveSHA256: artifact.Archive,
	}
	want := SoftnetBlockTarget == "@boxwarden-host-containment"
	if got := qualifiedSoftnet(candidate); got != want {
		t.Fatalf("candidate admission = %t, want %t for build %q", got, want, NetworkPolicyBuild)
	}
}

func TestSelectedSoftnetRequiresAllIdentityFields(t *testing.T) {
	selected := ToolIdentity{Path: QualifiedSoftnetPath, Version: SoftnetVersion, ExecutableSHA256: SoftnetExecutableSHA256, ArchiveSHA256: SoftnetArchiveSHA256}
	if !qualifiedSoftnet(selected) {
		t.Fatal("selected identity rejected")
	}
	for _, field := range []string{"path", "version", "executable", "archive"} {
		t.Run(field, func(t *testing.T) {
			wrong := selected
			switch field {
			case "path":
				wrong.Path = "/tmp/softnet"
			case "version":
				wrong.Version += "-other"
			case "executable":
				wrong.ExecutableSHA256 = strings.Repeat("0", 64)
			case "archive":
				wrong.ArchiveSHA256 = strings.Repeat("0", 64)
			}
			if qualifiedSoftnet(wrong) {
				t.Fatalf("admitted wrong %s", field)
			}
		})
	}
}
