package hostx

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func controlledClipboardIdentityForTest(path string) ToolIdentity {
	return ToolIdentity{Path: path, Version: ControlledClipboardTartVersion, ExecutableSHA256: ControlledClipboardTartExecutableSHA256, ArchiveSHA256: ControlledClipboardTartArchiveSHA256}
}
func TestControlledClipboardRequiresExactKnownIdentity(t *testing.T) {
	stock := qualifiedTartForTest()
	controlled := controlledClipboardIdentityForTest(stock.Path)
	if SupportsControlledClipboard(stock) || !qualifiedTart(stock) || !SupportsControlledClipboard(controlled) || !qualifiedTart(controlled) {
		t.Fatal("known identity capability mismatch")
	}
	for _, field := range []string{"version", "executable", "archive", "path"} {
		changed := controlled
		switch field {
		case "version":
			changed.Version = stock.Version
		case "executable":
			changed.ExecutableSHA256 = stock.ExecutableSHA256
		case "archive":
			changed.ArchiveSHA256 = strings.Repeat("0", 64)
		case "path":
			changed.Path = "relative/tart"
		}
		if SupportsControlledClipboard(changed) {
			t.Fatalf("mixed identity admitted: %s", field)
		}
		if field != "path" && qualifiedTart(changed) {
			t.Fatalf("unknown variant admitted: %s", field)
		}
	}
}
func setControlledManifest(t *testing.T, inspector *doctorInspectorFake, request Request) (string, Manifest) {
	t.Helper()
	path := filepath.Join(filepath.Dir(QualifiedSoftnetPath), "manifest.json")
	fact := inspector.paths[path]
	var m Manifest
	if err := json.Unmarshal(fact.Data, &m); err != nil {
		t.Fatal(err)
	}
	m.Tart = controlledClipboardIdentityForTest(request.TartPath)
	contents, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	fact.Data = contents
	inspector.paths[path] = fact
	tart := inspector.paths[request.TartPath]
	tart.SHA256 = ControlledClipboardTartExecutableSHA256
	inspector.paths[request.TartPath] = tart
	inspector.outputs[commandKey(request.TartPath, "--version")] = ControlledClipboardTartVersion
	return path, m
}
func TestDoctorAdmitsExactControlledManifestAndExecutable(t *testing.T) {
	inspector, request := healthyDoctorFixture(t)
	_, manifest := setControlledManifest(t, inspector, request)
	doctor := SystemDoctor{inspector: inspector}
	expectation, err := doctor.CheckRuntime(t.Context(), request)
	if err != nil || expectation.Manifest.Tart != manifest.Tart || !SupportsControlledClipboard(expectation.Manifest.Tart) {
		t.Fatalf("controlled admission: %+v %v", expectation, err)
	}
}
func TestDoctorSelectsDigestOnlyFromStrictMatchingManifest(t *testing.T) {
	for _, kind := range []string{"unknown field", "duplicate field", "wrong archive", "wrong version", "unsafe metadata", "wrong config path", "wrong executable"} {
		t.Run(kind, func(t *testing.T) {
			inspector, request := healthyDoctorFixture(t)
			path, m := setControlledManifest(t, inspector, request)
			fact := inspector.paths[path]
			switch kind {
			case "unknown field":
				fact.Data = []byte(strings.Replace(string(fact.Data), `"version":2`, `"version":2,"unknown":true`, 1))
			case "duplicate field":
				fact.Data = []byte(strings.Replace(string(fact.Data), `"version":2`, `"version":2,"version":2`, 1))
			case "wrong archive":
				m.Tart.ArchiveSHA256 = strings.Repeat("0", 64)
				fact.Data, _ = json.Marshal(m)
			case "wrong version":
				m.Tart.Version = TartVersion
				fact.Data, _ = json.Marshal(m)
			case "unsafe metadata":
				fact.Mode = 0600
			case "wrong config path":
				m.Tart.Path = "/other/tart"
				fact.Data, _ = json.Marshal(m)
			case "wrong executable":
				tool := inspector.paths[request.TartPath]
				tool.SHA256 = TartExecutableSHA256
				inspector.paths[request.TartPath] = tool
			}
			inspector.paths[path] = fact
			report := (SystemDoctor{inspector: inspector}).Doctor(t.Context(), request)
			if report.Status == Healthy {
				t.Fatal("invalid variant healthy")
			}
			if !hasFinding(report, "tart.digest") {
				t.Fatal("untrusted manifest selected variant digest")
			}
		})
	}
}
func TestControlledClipboardVariantNotAcceptedByOrdinaryInstall(t *testing.T) {
	request := InstallRequest{Version: InstallRequestVersion, SoftnetSource: "/source/softnet", Tart: controlledClipboardIdentityForTest("/opt/qualified/tart"), TartHome: "/Users/wes/tart"}
	if err := request.Validate(); err == nil {
		t.Fatal("ordinary installer accepts controlled variant")
	}
	if qualifiedTartFact(PathFact{Exists: true, Regular: true, Links: 1, Mode: 0755, SHA256: ControlledClipboardTartExecutableSHA256}) {
		t.Fatal("ordinary init accepts controlled source")
	}
}

func TestControlledClipboardStageR3ExactIdentity(t *testing.T) {
	identity := ToolIdentity{
		Path:             "/Library/Boxwarden/toolchains/tart/clipboard-r3/tart",
		Version:          "2.32.1-boxwarden-clipboard-r3",
		ExecutableSHA256: "1573e4be9a10087f8e5dce5dcc5718cfef4d5d0274c60d5e209ba1f13c49f7a6",
		ArchiveSHA256:    "b5f487c3b092b48d23819d2828c45b96f7c84816d51b5ca9dd68f3de64fa3a64",
	}
	if !SupportsControlledClipboard(identity) {
		t.Fatal("exact signed stage-r3 identity refused")
	}
	identity.Version = "2.32.1-boxwarden-clipboard-r2"
	if SupportsControlledClipboard(identity) {
		t.Fatal("superseded stage-r2 identity accepted")
	}
}
