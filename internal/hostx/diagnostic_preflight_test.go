//go:build (darwin || linux) && n1diagnostic && n1cleanup && !n1candidate

package hostx

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func diagnosticDoctorFixture(t *testing.T) (*doctorInspectorFake, Request) {
	f, old := healthyDoctorFixture(t)
	r := Request{ConfiguredStateRoots: []string{"/Volumes/BoxwardenAlphaQualification/n1-diagnostic-20260930-state"}, TartPath: "/Users/devel/Library/Application Support/boxwarden/toolchains/tart/2.32.1/tart.app/Contents/MacOS/tart", TartHome: "/Users/devel/Library/Application Support/boxwarden/tart", SoftnetPath: "/Users/devel/Backup/boxwarden_archive/n1-diagnostic-20260930/package/artifacts/softnet-diagnostic"}
	f.paths[r.TartPath] = f.paths[old.TartPath]
	delete(f.paths, old.TartPath)
	f.paths[r.TartHome] = f.paths[old.TartHome]
	delete(f.paths, old.TartHome)
	f.outputs[commandKey(r.TartPath, "--version")] = TartVersion
	f.operator = Operator{501, "devel", "/Users/devel"}
	f.group = Group{501, OperatorGroupName, []int{501}}
	f.effectiveGroups = []int{0}
	f.platform = PlatformFact{OS: QualifiedPlatform, Arch: QualifiedArch, Release: TrialMacOS, Build: TrialMacOSBuild}
	p := filepath.Join(filepath.Dir(QualifiedSoftnetPath), "manifest.json")
	fact := f.paths[p]
	var m Manifest
	if e := json.Unmarshal(fact.Data, &m); e != nil {
		t.Fatal(e)
	}
	m.Operator = f.operator
	m.Group = f.group
	m.Tart.Path = r.TartPath
	m.TartHome = r.TartHome
	fact.Data, _ = json.Marshal(m)
	stockPath := "/Library/Boxwarden/toolchains/softnet/0.19.0/ab333619fc8bd7277837545e49a771baa994c01c3e8c14904ae4cc4c1f37269e/softnet"
	m.Softnet = ToolIdentity{stockPath, "0.19.0", "ab333619fc8bd7277837545e49a771baa994c01c3e8c14904ae4cc4c1f37269e", "1612e1296834aae0b6389650c7c5190add1ee8d71474e328691e67679ecda53c"}
	fact.Data, _ = json.Marshal(m)
	delete(f.paths, p)
	delete(f.paths, QualifiedSoftnetPath)
	delete(f.paths, filepath.Join(filepath.Dir(QualifiedSoftnetPath), "launch.lock"))
	f.paths[filepath.Join(filepath.Dir(stockPath), "manifest.json")] = fact
	f.paths[stockPath] = PathFact{Exists: true, Regular: true, Mode: SoftnetMode, UID: 0, GID: 501, Links: 1, SHA256: m.Softnet.ExecutableSHA256}
	f.paths[filepath.Dir(stockPath)] = PathFact{Exists: true, Directory: true, Mode: 0755, UID: 0, GID: 0, Links: 1}
	f.paths[filepath.Dir(filepath.Dir(stockPath))] = f.paths[filepath.Dir(stockPath)]
	f.directoryNames = []string{"manifest.json", "softnet"}
	return f, r
}
func TestDiagnosticPreflightRootOperatorBridgeIsReadOnlyAndDoesNotFabricateGroups(t *testing.T) {
	for _, which := range []string{"healthy", "uid", "euid", "operator", "group", "members", "root", "candidate-source", "platform", "old-platform"} {
		t.Run(which, func(t *testing.T) {
			f, r := diagnosticDoctorFixture(t)
			uid, euid := 0, 0
			switch which {
			case "uid":
				uid = 501
			case "euid":
				euid = 501
			case "operator":
				f.operator.Name = "other"
			case "group":
				f.group.ID = 20
			case "members":
				f.group.Members = append(f.group.Members, 502)
			case "root":
				r.ConfiguredStateRoots[0] += "/sibling"
			case "candidate-source":
				r.SoftnetPath += ".other"
			case "old-platform":
				f.platform.Release = QualifiedMacOS
				f.platform.Build = QualifiedMacOSBuild
			case "platform":
				f.platform.Build = "wrong"
			}
			s := SystemDoctor{inspector: f}
			_, _, e := s.diagnosticPreflightAdmissionIDs(t.Context(), r, uid, euid)
			if which == "healthy" {
				if e != nil {
					t.Fatal(e, s.inspectSoftnetPolicy(t.Context(), r, false, stockPreflightPolicy()).report)
				}
				if _, ordinaryErr := s.CheckRuntime(t.Context(), r); ordinaryErr == nil {
					t.Fatal("ordinary admission incorrectly accepted root groups")
				}
			} else if e == nil {
				t.Fatal("unsafe qualification bridge")
			}
			if f.mutations != 0 {
				t.Fatal("read-only preflight mutated")
			}
			if len(f.effectiveGroups) != 1 || f.effectiveGroups[0] != 0 {
				t.Fatal("fabricated root groups")
			}
		})
	}
	if os.Getuid() != 0 || os.Geteuid() != 0 {
		f, r := diagnosticDoctorFixture(t)
		if (SystemDoctor{inspector: f}).CheckDiagnosticPreflight(t.Context(), r) == nil || len(f.commands) != 0 || len(f.operations) != 0 {
			t.Fatal("nonroot reached observation")
		}
	}
}

func TestDiagnosticOrdinaryManifestAndCleanupRemainDiagnosticOnly(t *testing.T) {
	f, r := diagnosticDoctorFixture(t)
	raw := f.paths[filepath.Join(filepath.Dir(stockPreflightPolicy().path), "manifest.json")].Data
	if _, e := ParseManifest(raw); e == nil {
		t.Fatal("ordinary diagnostic accepted stock manifest")
	}
	var m Manifest
	if json.Unmarshal(raw, &m) != nil || m.Validate() == nil {
		t.Fatal("ordinary validation policy widened")
	}
	if _, _, e := (SystemDoctor{inspector: f}).diagnosticAdmissionIDs(t.Context(), r, 0, 0, false); e == nil {
		t.Fatal("cleanup selected stock policy")
	}
	if f.paths[QualifiedSoftnetPath].Exists {
		t.Fatal("candidate fixture unexpectedly present")
	}
}
