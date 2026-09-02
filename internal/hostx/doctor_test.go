package hostx

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

func TestNewSystemDoctorConstructsAStandaloneReadOnlyDoctor(t *testing.T) {
	inspector, request := healthyDoctorFixture(t)
	doctor := NewSystemDoctor()
	doctor.inspector = inspector
	if report := doctor.Doctor(t.Context(), request); report.Status != Healthy {
		t.Fatalf("Doctor() status = %q, want healthy", report.Status)
	}
}

func TestDoctorReportsHealthyOnlyWhenEveryHostPrerequisiteMatches(t *testing.T) {
	inspector, request := healthyDoctorFixture(t)
	service := SystemService{inspector: inspector}
	report := service.Doctor(t.Context(), request)
	if report.Status != Healthy || len(report.Findings) != 0 {
		t.Fatalf("Doctor() = %#v, want healthy with no findings", report)
	}
	if inspector.mutations != 0 {
		t.Fatalf("doctor mutation count = %d, want zero", inspector.mutations)
	}
}

func TestDoctorReportsUnqualifiedBuildAndUnsupportedManifestSchema(t *testing.T) {
	inspector, request := healthyDoctorFixture(t)
	inspector.platform.Build = "25G84"
	report := (SystemService{inspector: inspector}).Doctor(t.Context(), request)
	if report.Status != Unsupported || !hasFinding(report, "platform.build") {
		t.Fatalf("Doctor() findings = %#v, want unsupported build", report.Findings)
	}

	inspector, request = healthyDoctorFixture(t)
	manifestPath := filepath.Join(filepath.Dir(QualifiedSoftnetPath), "manifest.json")
	fact := inspector.paths[manifestPath]
	legacy := strings.Replace(string(fact.Data), `"version":2`, `"version":1`, 1)
	legacy = strings.Replace(legacy, `"macos_build":"25G83",`, ``, 1)
	fact.Data = []byte(legacy)
	inspector.paths[manifestPath] = fact
	report = (SystemService{inspector: inspector}).Doctor(t.Context(), request)
	if report.Status != Unsupported || !hasFinding(report, "manifest.unsupported") {
		t.Fatalf("Doctor() findings = %#v, want unsupported manifest schema", report.Findings)
	}
}

func TestDoctorNeverExecutesConfiguredTartAndGatesScreenVersionOnExactIdentity(t *testing.T) {
	inspector, request := healthyDoctorFixture(t)
	if report := (SystemService{inspector: inspector}).Doctor(t.Context(), request); report.Status != Healthy {
		t.Fatalf("healthy Doctor() = %#v", report)
	}
	if containsString(inspector.commands, commandKey(request.TartPath, "--version")) {
		t.Fatalf("doctor executed configured Tart: %v", inspector.commands)
	}
	if !containsString(inspector.commands, commandKey(ScreenPath, "--version")) {
		t.Fatalf("doctor did not verify safe Screen: %v", inspector.commands)
	}

	for name, mutate := range map[string]func(*PathFact){
		"digest": func(fact *PathFact) { fact.SHA256 = strings.Repeat("0", 64) },
		"owner":  func(fact *PathFact) { fact.UID = 501 },
		"setuid": func(fact *PathFact) { fact.Mode = 0o4755 },
		"links":  func(fact *PathFact) { fact.Links = 2 },
	} {
		t.Run("screen "+name, func(t *testing.T) {
			inspector, request := healthyDoctorFixture(t)
			fact := inspector.paths[ScreenPath]
			mutate(&fact)
			inspector.paths[ScreenPath] = fact
			_ = (SystemService{inspector: inspector}).Doctor(t.Context(), request)
			if containsString(inspector.commands, commandKey(ScreenPath, "--version")) {
				t.Fatalf("doctor executed unsafe Screen after %s drift", name)
			}
		})
	}
}

func TestDoctorTartAdmissionRequiresExactSafeExecutableMetadata(t *testing.T) {
	for name, mutate := range map[string]func(*PathFact){
		"not executable": func(fact *PathFact) { fact.Mode = 0o644 },
		"setuid":         func(fact *PathFact) { fact.Mode = 0o4755 },
		"setgid":         func(fact *PathFact) { fact.Mode = 0o2755 },
		"digest":         func(fact *PathFact) { fact.SHA256 = strings.Repeat("0", 64) },
		"hardlink":       func(fact *PathFact) { fact.Links = 2 },
	} {
		t.Run(name, func(t *testing.T) {
			inspector, request := healthyDoctorFixture(t)
			fact := inspector.paths[request.TartPath]
			mutate(&fact)
			inspector.paths[request.TartPath] = fact
			report := (SystemService{inspector: inspector}).Doctor(t.Context(), request)
			if report.Status != Drifted {
				t.Fatalf("Doctor() status = %q, want unsafe Tart drift", report.Status)
			}
			if containsString(inspector.commands, commandKey(request.TartPath, "--version")) {
				t.Fatalf("doctor executed unsafe configured Tart: %v", inspector.commands)
			}
		})
	}
}

func TestDoctorSSHToolsRequireQualifiedSystemExecutableMetadata(t *testing.T) {
	for name, mutate := range map[string]func(*PathFact){
		"not executable": func(fact *PathFact) { fact.Mode = 0o644 },
		"setuid":         func(fact *PathFact) { fact.Mode = 0o4755 },
		"setgid":         func(fact *PathFact) { fact.Mode = 0o2755 },
		"not root owned": func(fact *PathFact) { fact.UID = 501 },
		"hardlink":       func(fact *PathFact) { fact.Links = 2 },
		"extended ACL":   func(fact *PathFact) { fact.ExtendedACL = true },
	} {
		for _, path := range []string{"/usr/bin/ssh", "/usr/bin/ssh-keygen"} {
			t.Run(path+"/"+name, func(t *testing.T) {
				inspector, request := healthyDoctorFixture(t)
				fact := inspector.paths[path]
				mutate(&fact)
				inspector.paths[path] = fact

				report := (SystemService{inspector: inspector}).Doctor(t.Context(), request)
				if report.Status != Drifted {
					t.Fatalf("Doctor() status = %q, want unsafe %s drift", report.Status, path)
				}
			})
		}
	}
}

func TestDoctorClassifiesExistingUnverifiableAncestorAndTartHomeAsDrifted(t *testing.T) {
	for code, path := range map[string]string{
		"softnet.ancestor.Library.Boxwarden": "/Library/Boxwarden",
		"tart-home.unsafe":                   "/Users/wes/tart",
	} {
		t.Run(code, func(t *testing.T) {
			inspector, request := healthyDoctorFixture(t)
			inspector.failures[path] = errors.New("sensitive inspection detail")
			report := (SystemService{inspector: inspector}).Doctor(t.Context(), request)
			for _, finding := range report.Findings {
				if finding.Code == code && finding.Category == Drifted && !strings.Contains(fmt.Sprint(finding), "sensitive") {
					return
				}
			}
			t.Fatalf("Doctor() findings = %#v, want redacted %s drift", report.Findings, code)
		})
	}
}

func TestDoctorReportsAllCategoriesInStableOrderWithoutSensitiveErrors(t *testing.T) {
	inspector, request := healthyDoctorFixture(t)
	inspector.platform.OS = "linux"
	softnet := inspector.paths[QualifiedSoftnetPath]
	softnet.Mode = 0o755
	inspector.paths[QualifiedSoftnetPath] = softnet
	inspector.effectiveGroups = nil
	delete(inspector.paths, "/usr/bin/ssh-keygen")
	inspector.homebrew = []HomebrewSoftnet{{Path: "/opt/homebrew/bin/softnet", Privilege: "setuid"}}
	inspector.failures["/usr/bin/ssh"] = fmt.Errorf("secret-token-must-not-leak")

	report := (SystemService{inspector: inspector}).Doctor(t.Context(), request)
	if report.Status != Unsupported {
		t.Fatalf("status = %q, want highest-severity unsupported", report.Status)
	}
	var codes []string
	for _, finding := range report.Findings {
		codes = append(codes, finding.Code)
		combined := finding.Observed + finding.Expected + finding.Remedy
		if strings.Contains(combined, "secret-token") {
			t.Fatalf("finding leaked dependency error: %#v", finding)
		}
	}
	wantPresent := []string{"group.not-effective", "homebrew.softnet-privileged", "platform.unsupported", "softnet.mode", "ssh.metadata", "ssh-keygen.missing"}
	for _, want := range wantPresent {
		if !containsString(codes, want) {
			t.Fatalf("finding codes %v do not contain %q", codes, want)
		}
	}
	if !sort.StringsAreSorted(codes) {
		t.Fatalf("finding codes are not deterministic: %v", codes)
	}
}

func TestDoctorClassifiesExistingUnverifiableToolOrManifestAsDrifted(t *testing.T) {
	for code, path := range map[string]string{
		"tart":     "/opt/qualified/tart",
		"manifest": filepath.Join(filepath.Dir(QualifiedSoftnetPath), "manifest.json"),
		"softnet":  QualifiedSoftnetPath,
	} {
		t.Run(code, func(t *testing.T) {
			inspector, request := healthyDoctorFixture(t)
			inspector.failures[path] = errors.New("sensitive inspection detail")

			report := (SystemService{inspector: inspector}).Doctor(t.Context(), request)
			var matched *Finding
			for index := range report.Findings {
				if report.Findings[index].Code == code+".metadata" {
					matched = &report.Findings[index]
				}
				if report.Findings[index].Code == code+".missing" {
					t.Fatalf("Doctor() classified existing unsafe %s as missing: %#v", code, report.Findings[index])
				}
			}
			if matched == nil || matched.Category != Drifted {
				t.Fatalf("Doctor() findings = %#v, want %s.metadata drift", report.Findings, code)
			}
			if matched.Observed != "path exists but safety inspection failed" || matched.Remedy != "inspect host tool state manually" {
				t.Fatalf("unsafe-path finding = %#v, want stable redacted diagnostic", matched)
			}
			if strings.Contains(fmt.Sprint(*matched), "sensitive") {
				t.Fatalf("unsafe-path finding leaked inspection error: %#v", matched)
			}
		})
	}
}

func TestOSDoctorInspectorPreservesExistenceForUnsafeLinkedFiles(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(base, "tool")
	if err := os.WriteFile(target, []byte("tool"), 0o755); err != nil {
		t.Fatal(err)
	}
	inspector := &osDoctorInspector{acl: noACLInspector{}}

	symlink := filepath.Join(base, "tool-link")
	if err := os.Symlink(target, symlink); err != nil {
		t.Fatal(err)
	}
	if fact, err := inspector.InspectPath(symlink); err == nil || !fact.Exists {
		t.Fatalf("InspectPath(symlink) = %#v, %v; want existing unsafe result", fact, err)
	}

	hardlink := filepath.Join(base, "tool-hardlink")
	if err := os.Link(target, hardlink); err != nil {
		t.Fatal(err)
	}
	if fact, err := inspector.InspectPath(target); err == nil || !fact.Exists || fact.Links != 2 {
		t.Fatalf("InspectPath(hardlink) = %#v, %v; want existing two-link unsafe result", fact, err)
	}
}

func TestDoctorTreatsPrivilegedHomebrewSoftnetAsBlockingDrift(t *testing.T) {
	inspector, request := healthyDoctorFixture(t)
	inspector.homebrew = []HomebrewSoftnet{{Path: "/usr/local/bin/softnet", Privilege: "passwordless-root"}}
	report := (SystemService{inspector: inspector}).Doctor(t.Context(), request)
	if report.Status != Drifted || len(report.Findings) != 1 || report.Findings[0].Code != "homebrew.softnet-privileged" {
		t.Fatalf("Doctor() = %#v, want one blocking Homebrew finding", report)
	}
}

func TestDoctorRejectsMutableSoftnetCurrentPointer(t *testing.T) {
	inspector, request := healthyDoctorFixture(t)
	inspector.paths["/Library/Boxwarden/toolchains/softnet/current"] = PathFact{Exists: true}
	report := (SystemService{inspector: inspector}).Doctor(t.Context(), request)
	if report.Status != Drifted || len(report.Findings) != 1 || report.Findings[0].Code != "softnet.current" {
		t.Fatalf("Doctor() = %#v, want current-pointer drift", report)
	}
}

func TestDoctorRejectsHostAndConfiguredStatePathOverlap(t *testing.T) {
	inspector, request := healthyDoctorFixture(t)
	request.ConfiguredStateRoots = []string{"/opt"}
	report := (SystemService{inspector: inspector}).Doctor(t.Context(), request)
	if report.Status != Drifted {
		t.Fatalf("Doctor() status = %q, want drift", report.Status)
	}
	found := false
	for _, finding := range report.Findings {
		found = found || finding.Code == "paths.overlap"
	}
	if !found {
		t.Fatalf("Doctor() findings = %#v, want paths.overlap", report.Findings)
	}
}

func TestDoctorAndInitAgreeOnEveryHostPathOverlap(t *testing.T) {
	for name, overlap := range map[string]func(*Request){
		"Tart and tart home": func(request *Request) { request.TartHome = request.TartPath },
		"Tart and Softnet":   func(request *Request) { request.SoftnetPath = request.TartPath },
		"tart home and Softnet": func(request *Request) {
			request.SoftnetPath = request.TartHome
		},
	} {
		t.Run(name, func(t *testing.T) {
			inspector, request := healthyDoctorFixture(t)
			overlap(&request)
			report := (SystemDoctor{inspector: inspector}).Doctor(t.Context(), request)
			if report.Status != Drifted || !hasFinding(report, "paths.overlap") {
				t.Fatalf("Doctor() report = %#v, want paths.overlap drift", report)
			}

			runner := &privilegeRunnerFake{}
			initializer := SystemInitializer{inspector: inspector, privilege: runner, executable: "/opt/boxwarden/bin/boxwarden", sourceValidator: func(string, string) error { return nil }}
			if _, err := initializer.Init(t.Context(), request); err == nil {
				t.Fatal("Init() error = nil, want host path overlap refusal")
			}
			if runner.command.Path != "" {
				t.Fatalf("Init() invoked privilege runner for overlapping host paths: %#v", runner.command)
			}
		})
	}
}

func TestDoctorAndInitCheckEveryConfiguredStateRoot(t *testing.T) {
	for name, root := range map[string]string{
		"later root overlaps Tart":            "/opt",
		"later root overlaps fixed toolchain": "/Library",
	} {
		t.Run(name, func(t *testing.T) {
			inspector, request := healthyDoctorFixture(t)
			request.ConfiguredStateRoots = []string{"/Users/wes/state/work", root}
			report := (SystemDoctor{inspector: inspector}).Doctor(t.Context(), request)
			if report.Status != Drifted || !hasFinding(report, "paths.overlap") {
				t.Fatalf("Doctor() report = %#v, want later-root paths.overlap drift", report)
			}

			runner := &privilegeRunnerFake{}
			initializer := SystemInitializer{inspector: inspector, privilege: runner, executable: "/opt/boxwarden/bin/boxwarden", sourceValidator: func(string, string) error { return nil }}
			if _, err := initializer.Init(t.Context(), request); err == nil {
				t.Fatal("Init() error = nil, want later-root overlap refusal")
			}
			if runner.command.Path != "" {
				t.Fatalf("Init() invoked privilege runner for later-root overlap: %#v", runner.command)
			}
		})
	}
}

func hasFinding(report Report, code string) bool {
	for _, finding := range report.Findings {
		if finding.Code == code {
			return true
		}
	}
	return false
}

func healthyDoctorFixture(t *testing.T) (*doctorInspectorFake, Request) {
	t.Helper()
	request := Request{ConfiguredStateRoots: []string{"/Users/wes/state/work"}, TartPath: "/opt/qualified/tart", TartHome: "/Users/wes/tart", SoftnetPath: "/opt/homebrew/Cellar/softnet/0.19.0/bin/softnet"}
	group := Group{ID: 20, Name: OperatorGroupName, Members: []int{501}}
	operator := Operator{UID: 501, Name: "wes", Home: "/Users/wes"}
	manifest := Manifest{
		Version: ManifestVersion, Platform: QualifiedPlatform, MacOS: QualifiedMacOS, MacOSBuild: QualifiedMacOSBuild,
		Tart:    ToolIdentity{Path: request.TartPath, Version: TartVersion, ExecutableSHA256: TartExecutableSHA256, ArchiveSHA256: TartArchiveSHA256},
		Softnet: ToolIdentity{Path: QualifiedSoftnetPath, Version: SoftnetVersion, ExecutableSHA256: SoftnetExecutableSHA256, ArchiveSHA256: SoftnetArchiveSHA256},
		RootUID: 0, Group: group, Operator: operator, TartHome: request.TartHome,
		SoftnetMode: SoftnetMode, InstalledAt: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
	}
	manifestBytes, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	paths := map[string]PathFact{}
	for _, path := range trustedSoftnetDirectories() {
		paths[path] = PathFact{Exists: true, Directory: true, Mode: 0o755, UID: 0, GID: 0, Links: 1}
	}
	paths[QualifiedSoftnetPath] = PathFact{Exists: true, Regular: true, Mode: SoftnetMode, UID: 0, GID: group.ID, Links: 1, SHA256: SoftnetExecutableSHA256}
	paths[filepath.Join(filepath.Dir(QualifiedSoftnetPath), "manifest.json")] = PathFact{Exists: true, Regular: true, Mode: 0o400, UID: 0, GID: 0, Links: 1, Data: manifestBytes}
	paths[request.TartPath] = PathFact{Exists: true, Regular: true, Mode: 0o755, UID: 501, GID: 20, Links: 1, SHA256: TartExecutableSHA256}
	paths[request.TartHome] = PathFact{Exists: true, Directory: true, Mode: 0o700, UID: 501, GID: 20, Links: 1}
	paths["/usr/bin/ssh"] = PathFact{Exists: true, Regular: true, Mode: 0o755, UID: 0, GID: 0, Links: 1}
	paths["/usr/bin/ssh-keygen"] = PathFact{Exists: true, Regular: true, Mode: 0o755, UID: 0, GID: 0, Links: 1}
	paths[ScreenPath] = PathFact{Exists: true, Regular: true, Mode: 0o755, UID: 0, GID: 0, Links: 1, SHA256: ScreenExecutableSHA256}
	return &doctorInspectorFake{
		platform:        PlatformFact{OS: QualifiedPlatform, Arch: QualifiedArch, Release: QualifiedMacOS, Build: QualifiedMacOSBuild},
		paths:           paths,
		operator:        operator,
		group:           group,
		effectiveGroups: []int{group.ID},
		outputs: map[string]string{
			commandKey(request.TartPath, "--version"): TartVersion,
			commandKey(ScreenPath, "--version"):       ScreenVersionOutput,
		},
		failures: map[string]error{},
	}, request
}

type doctorInspectorFake struct {
	platform        PlatformFact
	paths           map[string]PathFact
	operator        Operator
	group           Group
	effectiveGroups []int
	outputs         map[string]string
	homebrew        []HomebrewSoftnet
	failures        map[string]error
	commands        []string
	mutations       int
}

func (f *doctorInspectorFake) Platform() PlatformFact { return f.platform }
func (f *doctorInspectorFake) InspectPath(path string) (PathFact, error) {
	if err := f.failures[path]; err != nil {
		return f.paths[path], err
	}
	return f.paths[path], nil
}
func (f *doctorInspectorFake) LookupOperator(int) (Operator, error) { return f.operator, nil }
func (f *doctorInspectorFake) ExactOperatorGroup(operator Operator, _ string) (Group, error) {
	if operator != f.operator {
		return Group{}, errors.New("operator mismatch")
	}
	return f.group, nil
}
func (f *doctorInspectorFake) EffectiveGroups() ([]int, error) {
	return append([]int(nil), f.effectiveGroups...), nil
}
func (f *doctorInspectorFake) CommandOutput(path string, args ...string) (string, error) {
	f.commands = append(f.commands, commandKey(path, args...))
	return f.outputs[commandKey(path, args...)], nil
}
func (f *doctorInspectorFake) HomebrewSoftnet() ([]HomebrewSoftnet, error) {
	return append([]HomebrewSoftnet(nil), f.homebrew...), nil
}

func commandKey(path string, args ...string) string {
	return path + "\x00" + strings.Join(args, "\x00")
}
func containsString(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}
