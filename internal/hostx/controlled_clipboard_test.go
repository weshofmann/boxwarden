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

func TestControlledClipboardStageR4ExactIdentity(t *testing.T) {
	identity := ToolIdentity{
		Path:             "/Library/Boxwarden/toolchains/tart/clipboard-r4/tart",
		Version:          "2.32.1-boxwarden-clipboard-r4",
		ExecutableSHA256: "46e809c95260d6a264b15662bd2117eddd13b0a0ca19dcdc6bae244cc7799fc2",
		ArchiveSHA256:    "4126636c097dffaefff70c0abec116623885554419c87825b4e9e458a9f987ff",
	}
	if !SupportsControlledClipboard(identity) || !qualifiedTart(identity) {
		t.Fatal("exact signed stage-r4 identity refused")
	}
	identity.ArchiveSHA256 = ControlledClipboardTartArchiveSHA256
	if SupportsControlledClipboard(identity) || qualifiedTart(identity) {
		t.Fatal("mixed r3/r4 identity accepted")
	}
}

func TestDoctorAdmitsOnlyExactR4ManifestAndExecutable(t *testing.T) {
	inspector, request := healthyDoctorFixture(t)
	path, manifest := setControlledManifest(t, inspector, request)
	manifest.Tart.Version = ControlledClipboardTartR4Version
	manifest.Tart.ExecutableSHA256 = ControlledClipboardTartR4ExecutableSHA256
	manifest.Tart.ArchiveSHA256 = ControlledClipboardTartR4ArchiveSHA256
	fact := inspector.paths[path]
	fact.Data, _ = json.Marshal(manifest)
	inspector.paths[path] = fact
	tool := inspector.paths[request.TartPath]
	tool.SHA256 = ControlledClipboardTartR4ExecutableSHA256
	inspector.paths[request.TartPath] = tool
	inspector.outputs[commandKey(request.TartPath, "--version")] = ControlledClipboardTartR4Version
	if report := (SystemDoctor{inspector: inspector}).Doctor(t.Context(), request); report.Status != Healthy {
		t.Fatalf("exact r4 manifest and runtime refused: %+v", report)
	}
	tool.SHA256 = ControlledClipboardTartExecutableSHA256
	inspector.paths[request.TartPath] = tool
	if report := (SystemDoctor{inspector: inspector}).Doctor(t.Context(), request); report.Status == Healthy {
		t.Fatal("r4 manifest accepted with r3 executable")
	}
}

func TestControlledClipboardStageR5ExactIdentity(t *testing.T) {
	identity := ToolIdentity{
		Path:             "/Library/Boxwarden/toolchains/tart/clipboard-r5/tart",
		Version:          "2.32.1-boxwarden-clipboard-r5",
		ExecutableSHA256: "e0047ddb7ffff0967591a1bd03374980f1775bdb4ab44ffd5b9bfb4700d7b97b",
		ArchiveSHA256:    "3a58485df6a10958e62da1fd2692c47cf54ddec0448338695b0daf327d7617bc",
	}
	if !SupportsControlledClipboard(identity) || !qualifiedTart(identity) {
		t.Fatal("exact signed stage-r5 identity refused")
	}
	identity.ArchiveSHA256 = ControlledClipboardTartR4ArchiveSHA256
	if SupportsControlledClipboard(identity) || qualifiedTart(identity) {
		t.Fatal("mixed r4/r5 identity accepted")
	}
}

func TestDoctorAdmitsOnlyExactR5ManifestAndExecutable(t *testing.T) {
	inspector, request := healthyDoctorFixture(t)
	path, manifest := setControlledManifest(t, inspector, request)
	manifest.Tart.Version = "2.32.1-boxwarden-clipboard-r5"
	manifest.Tart.ExecutableSHA256 = "e0047ddb7ffff0967591a1bd03374980f1775bdb4ab44ffd5b9bfb4700d7b97b"
	manifest.Tart.ArchiveSHA256 = "3a58485df6a10958e62da1fd2692c47cf54ddec0448338695b0daf327d7617bc"
	fact := inspector.paths[path]
	fact.Data, _ = json.Marshal(manifest)
	inspector.paths[path] = fact
	tool := inspector.paths[request.TartPath]
	tool.SHA256 = manifest.Tart.ExecutableSHA256
	inspector.paths[request.TartPath] = tool
	inspector.outputs[commandKey(request.TartPath, "--version")] = manifest.Tart.Version
	if report := (SystemDoctor{inspector: inspector}).Doctor(t.Context(), request); report.Status != Healthy {
		t.Fatalf("exact r5 manifest and runtime refused: %+v", report)
	}
	tool.SHA256 = ControlledClipboardTartR4ExecutableSHA256
	inspector.paths[request.TartPath] = tool
	if report := (SystemDoctor{inspector: inspector}).Doctor(t.Context(), request); report.Status == Healthy {
		t.Fatal("r5 manifest accepted with r4 executable")
	}
}

func TestControlledClipboardStageR6ExactIdentity(t *testing.T) {
	identity := ToolIdentity{
		Path:             "/Library/Boxwarden/toolchains/tart/clipboard-r6/tart",
		Version:          "2.32.1-boxwarden-clipboard-r6",
		ExecutableSHA256: "e6d6894b793a6e3636e7438756a48fbdba35bf9885c4e08db7ad7bef8c118c99",
		ArchiveSHA256:    "899773a1dfec8d66c9a42f68ab105c2912b751cd4da3c2883ca2a83db5e355f0",
	}
	if !SupportsControlledClipboard(identity) || !qualifiedTart(identity) {
		t.Fatal("exact signed stage-r6 identity refused")
	}
	identity.ArchiveSHA256 = ControlledClipboardTartR5ArchiveSHA256
	if SupportsControlledClipboard(identity) || qualifiedTart(identity) {
		t.Fatal("mixed r5/r6 identity accepted")
	}
}

func TestDoctorAdmitsOnlyExactR6ManifestAndExecutable(t *testing.T) {
	inspector, request := healthyDoctorFixture(t)
	path, manifest := setControlledManifest(t, inspector, request)
	manifest.Tart.Version = "2.32.1-boxwarden-clipboard-r6"
	manifest.Tart.ExecutableSHA256 = "e6d6894b793a6e3636e7438756a48fbdba35bf9885c4e08db7ad7bef8c118c99"
	manifest.Tart.ArchiveSHA256 = "899773a1dfec8d66c9a42f68ab105c2912b751cd4da3c2883ca2a83db5e355f0"
	fact := inspector.paths[path]
	fact.Data, _ = json.Marshal(manifest)
	inspector.paths[path] = fact
	tool := inspector.paths[request.TartPath]
	tool.SHA256 = manifest.Tart.ExecutableSHA256
	inspector.paths[request.TartPath] = tool
	inspector.outputs[commandKey(request.TartPath, "--version")] = manifest.Tart.Version
	if report := (SystemDoctor{inspector: inspector}).Doctor(t.Context(), request); report.Status != Healthy {
		t.Fatalf("exact r6 manifest and runtime refused: %+v", report)
	}
	tool.SHA256 = ControlledClipboardTartR5ExecutableSHA256
	inspector.paths[request.TartPath] = tool
	if report := (SystemDoctor{inspector: inspector}).Doctor(t.Context(), request); report.Status == Healthy {
		t.Fatal("r6 manifest accepted with r5 executable")
	}
}
