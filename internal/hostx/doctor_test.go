package hostx

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

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

func TestDoctorRejectsHostAndDomainPathOverlap(t *testing.T) {
	inspector, request := healthyDoctorFixture(t)
	request.StateRoot = "/opt"
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

func healthyDoctorFixture(t *testing.T) (*doctorInspectorFake, Request) {
	t.Helper()
	request := Request{Domain: "work", StateRoot: "/Users/wes/state/work", TartPath: "/opt/qualified/tart", TartHome: "/Users/wes/tart", SoftnetPath: "/opt/homebrew/Cellar/softnet/0.19.0/bin/softnet"}
	group := Group{ID: 20, Name: OperatorGroupName, Members: []int{501}}
	operator := Operator{UID: 501, Name: "wes", Home: "/Users/wes"}
	manifest := Manifest{
		Version: 1, Platform: QualifiedPlatform, MacOS: QualifiedMacOS,
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
		platform:        PlatformFact{OS: QualifiedPlatform, Arch: QualifiedArch, Release: QualifiedMacOS},
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
func (f *doctorInspectorFake) LookupGroup(string) (Group, error)    { return f.group, nil }
func (f *doctorInspectorFake) IsMember(operator Operator, group Group) (bool, error) {
	return reflect.DeepEqual(operator, f.operator) && reflect.DeepEqual(group, f.group), nil
}
func (f *doctorInspectorFake) EffectiveGroups() ([]int, error) {
	return append([]int(nil), f.effectiveGroups...), nil
}
func (f *doctorInspectorFake) CommandOutput(path string, args ...string) (string, error) {
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
