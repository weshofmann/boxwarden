//go:build n1diagnostic && !n1candidate

package hostx

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestDiagnosticArtifactIdentityMatchesExactAdmission(t *testing.T) {
	data, err := os.ReadFile("../../tools/n1-diagnostic-softnet/artifact_v2.json")
	if err != nil {
		t.Fatal(err)
	}
	var a struct {
		Version    string `json:"version"`
		Executable string `json:"executable_sha256"`
		Archive    string `json:"archive_sha256"`
	}
	if err = json.Unmarshal(data, &a); err != nil {
		t.Fatal(err)
	}
	if a.Version != "0.19.0-boxwarden-n1-diagnostic.2" {
		t.Fatal("diagnostic version missing")
	}
	if a.Version != SoftnetVersion || a.Executable != SoftnetExecutableSHA256 || a.Archive != SoftnetArchiveSHA256 {
		t.Fatal("diagnostic artifact and compiled identity differ")
	}
	identity := ToolIdentity{Path: filepath.Join("/Library/Boxwarden/toolchains/softnet", a.Version, a.Executable, "softnet"), Version: a.Version, ExecutableSHA256: a.Executable, ArchiveSHA256: a.Archive}
	if !qualifiedSoftnet(identity) {
		t.Fatal("exact diagnostic artifact rejected")
	}
	for _, file := range []string{"../../tools/n1-softnet/artifact.json", "../../tools/n1-diagnostic-softnet/artifact.json"} {
		data, err = os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if err = json.Unmarshal(data, &a); err != nil {
			t.Fatal(err)
		}
		wrong := ToolIdentity{Path: filepath.Join("/Library/Boxwarden/toolchains/softnet", a.Version, a.Executable, "softnet"), Version: a.Version, ExecutableSHA256: a.Executable, ArchiveSHA256: a.Archive}
		if file == "../../tools/n1-diagnostic-softnet/artifact.json" {
			wrong.Path = "/private/tmp/softnet"
		}
		if qualifiedSoftnet(wrong) {
			t.Fatalf("admitted canonical or copied identity: %s", file)
		}
	}
	stock := ToolIdentity{Path: "/Library/Boxwarden/toolchains/softnet/0.19.0/ab333619fc8bd7277837545e49a771baa994c01c3e8c14904ae4cc4c1f37269e/softnet", Version: "0.19.0", ExecutableSHA256: "ab333619fc8bd7277837545e49a771baa994c01c3e8c14904ae4cc4c1f37269e", ArchiveSHA256: "1612e1296834aae0b6389650c7c5190add1ee8d71474e328691e67679ecda53c"}
	if qualifiedSoftnet(stock) {
		t.Fatal("diagnostic admitted stock")
	}
}

func TestReviewedDiagnostic2ExactAdmission(t *testing.T) {
	raw, e := os.ReadFile("../../tools/n1-diagnostic-softnet/artifact_v2.json")
	if e != nil {
		t.Fatal(e)
	}
	var a struct {
		Version    string `json:"version"`
		Executable string `json:"executable_sha256"`
		Archive    string `json:"archive_sha256"`
	}
	if json.Unmarshal(raw, &a) != nil {
		t.Fatal("metadata")
	}
	identity := ToolIdentity{Path: filepath.Join("/Library/Boxwarden/toolchains/softnet", a.Version, a.Executable, "softnet"), Version: a.Version, ExecutableSHA256: a.Executable, ArchiveSHA256: a.Archive}
	if !qualifiedSoftnet(identity) {
		t.Fatal("published reviewed diagnostic.2 exact identity rejected")
	}
}

func TestLegacyDiagnostic1ExactPathIsNotAdmittedAsDiagnostic2(t *testing.T) {
	raw, e := os.ReadFile("../../tools/n1-diagnostic-softnet/artifact.json")
	if e != nil {
		t.Fatal(e)
	}
	var a struct {
		Version    string `json:"version"`
		Executable string `json:"executable_sha256"`
		Archive    string `json:"archive_sha256"`
	}
	if json.Unmarshal(raw, &a) != nil {
		t.Fatal("metadata")
	}
	identity := ToolIdentity{Path: filepath.Join("/Library/Boxwarden/toolchains/softnet", a.Version, a.Executable, "softnet"), Version: a.Version, ExecutableSHA256: a.Executable, ArchiveSHA256: a.Archive}
	if qualifiedSoftnet(identity) {
		t.Fatal("legacy exact .1 identity adopted under .2")
	}
}
