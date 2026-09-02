package hostx

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/weshofmann/boxwarden/internal/execx"
)

const (
	ScreenPath             = "/usr/bin/screen"
	ScreenExecutableSHA256 = "07b706b76c0e7374eb524f9e2e738437f208b4b123d7d9b7b2666019c8881add"
	ScreenVersionOutput    = "Screen version 4.00.03 (FAU) 23-Oct-06"
)

type Status string

const (
	Healthy     Status = "healthy"
	Missing     Status = "missing/uninitialized"
	Drifted     Status = "drifted/unsafe"
	Unsupported Status = "unsupported/unqualified"
)

type Finding struct {
	Code     string
	Category Status
	Observed string
	Expected string
	Remedy   string
}
type Report struct {
	Status   Status
	Findings []Finding
}

func (r *Report) Normalize() {
	sort.Slice(r.Findings, func(i, j int) bool { return r.Findings[i].Code < r.Findings[j].Code })
	if len(r.Findings) == 0 {
		r.Status = Healthy
		return
	}
	status := Missing
	for _, finding := range r.Findings {
		if statusSeverity(finding.Category) > statusSeverity(status) {
			status = finding.Category
		}
	}
	r.Status = status
}

func statusSeverity(status Status) int {
	switch status {
	case Unsupported:
		return 3
	case Drifted:
		return 2
	case Missing:
		return 1
	default:
		return 0
	}
}

type Request struct {
	Domain      string
	StateRoot   string
	TartPath    string
	TartHome    string
	SoftnetPath string
}
type InitResult struct {
	HostInstalled       bool
	DomainInitialized   bool
	RefreshLoginSession bool
}
type Service interface {
	Init(context.Context, Request) (InitResult, error)
	Doctor(context.Context, Request) Report
}

// DoctorInspector is intentionally read-only. Doctor cannot obtain a privilege
// runner, initializer, mutation method, or raw private material through it.
type DoctorInspector interface {
	Platform() PlatformFact
	InspectPath(string) (PathFact, error)
	LookupOperator(int) (Operator, error)
	LookupGroup(string) (Group, error)
	IsMember(Operator, Group) (bool, error)
	EffectiveGroups() ([]int, error)
	CommandOutput(string, ...string) (string, error)
	HomebrewSoftnet() ([]HomebrewSoftnet, error)
}

type PlatformFact struct{ OS, Arch, Release string }
type PathFact struct {
	Exists, Regular, Directory bool
	Mode                       uint32
	UID, GID                   int
	Links                      uint64
	SHA256                     string
	ExtendedACL                bool
	Data                       []byte
}
type HomebrewSoftnet struct{ Path, Privilege string }

type SystemService struct {
	inspector       DoctorInspector
	privilege       PrivilegeRunner
	executable      string
	sourceValidator func(string, string) error
}

func NewSystemService() SystemService {
	return SystemService{inspector: NewOSDoctorInspector(), privilege: execx.OSRunner{MaxOutputBytes: 16 << 10, MaxStdinBytes: maxInstallRequestBytes}}
}

func (s SystemService) Doctor(_ context.Context, request Request) Report {
	inspector := s.inspector
	if inspector == nil {
		inspector = NewOSDoctorInspector()
	}
	report := Report{}
	add := func(code string, category Status, observed, expected, remedy string) {
		report.Findings = append(report.Findings, Finding{Code: code, Category: category, Observed: observed, Expected: expected, Remedy: remedy})
	}

	platform := inspector.Platform()
	if platform.OS != QualifiedPlatform || platform.Arch != QualifiedArch {
		add("platform.unsupported", Unsupported, platform.OS+"/"+platform.Arch, QualifiedPlatform+"/"+QualifiedArch, "use the qualified M1A host")
	}
	if platform.Release != QualifiedMacOS {
		add("platform.release", Unsupported, publicValue(platform.Release), QualifiedMacOS, "use the exact qualified macOS release")
	}
	if !canonicalAbsolute(request.StateRoot) {
		add("paths.state-root", Drifted, "noncanonical state root", "canonical absolute domain state root", "correct domain configuration")
	} else {
		paths := []string{request.TartPath, request.TartHome, request.SoftnetPath, productionToolchainPath()}
		for _, path := range paths {
			if canonicalAbsolute(path) && pathsOverlap(request.StateRoot, path) {
				add("paths.overlap", Drifted, "host/domain paths overlap", "disjoint host and domain roots", "correct version-2 host configuration")
				break
			}
		}
	}
	if !canonicalAbsolute(request.TartPath) {
		add("tart.path", Drifted, "noncanonical configured path", "canonical absolute Tart path", "correct version-2 host configuration")
	} else {
		tartFact, _ := checkTool(inspector, &report, "tart", request.TartPath, TartExecutableSHA256, 0, -1, -1)
		if tartFact.Exists && tartFact.Mode&0o111 == 0 {
			add("tart.mode", Drifted, fmt.Sprintf("%04o", tartFact.Mode), "executable", "install the exact qualified Tart executable")
		}
		if output, err := inspector.CommandOutput(request.TartPath, "--version"); err != nil || !strings.Contains(output, TartVersion) {
			add("tart.version", Drifted, "version did not match", TartVersion, "install the exact qualified Tart executable")
		}
	}

	manifestPath := filepath.Join(filepath.Dir(QualifiedSoftnetPath), "manifest.json")
	manifestFact, manifestOK := checkTool(inspector, &report, "manifest", manifestPath, "", manifestMode, 0, 0)
	var manifest Manifest
	if manifestOK {
		parsed, err := ParseManifest(manifestFact.Data)
		if err != nil {
			add("manifest.invalid", Drifted, "manifest failed strict validation", "exact versioned host manifest", "inspect the installed digest tree manually")
		} else {
			manifest = parsed
		}
	}
	for _, directory := range trustedSoftnetDirectories() {
		fact, err := inspector.InspectPath(directory)
		if err != nil || !fact.Exists {
			add("softnet.ancestor."+stablePathCode(directory), Missing, "missing or unreadable", "root-owned direct directory", "run explicit attended init")
			continue
		}
		exactInstalled := directory != "/Library"
		wrongGroupOrMode := exactInstalled && (fact.GID != 0 || fact.Mode != trustedDirectoryMode)
		if !fact.Directory || fact.UID != 0 || fact.Mode&0o022 != 0 || fact.ExtendedACL || wrongGroupOrMode {
			add("softnet.ancestor."+stablePathCode(directory), Drifted, "unsafe type, owner, mode, or ACL", "root-owned non-writable directory without ACL", "inspect the installed tree manually")
		}
	}
	currentPath := filepath.Join("/Library/Boxwarden/toolchains/softnet", "current")
	if fact, err := inspector.InspectPath(currentPath); err != nil {
		add("softnet.current", Drifted, "pointer check unavailable", "no mutable current pointer", "inspect the installed tree manually")
	} else if fact.Exists {
		add("softnet.current", Drifted, "mutable current pointer exists", "no mutable current pointer", "remove only after attended manual inspection")
	}
	softnetFact, softnetOK := checkTool(inspector, &report, "softnet", QualifiedSoftnetPath, SoftnetExecutableSHA256, SoftnetMode, 0, -1)
	if softnetOK && manifest.Version == 1 {
		if softnetFact.GID != manifest.Group.ID {
			add("softnet.group", Drifted, fmt.Sprintf("gid %d", softnetFact.GID), fmt.Sprintf("manifested gid %d", manifest.Group.ID), "inspect the installed tree manually")
		}
		if manifest.Tart.Path != request.TartPath || manifest.TartHome != request.TartHome {
			add("manifest.configuration", Drifted, "manifest/configuration mismatch", "paired Tart path and tart_home", "use the configuration that was explicitly initialized")
		}
		operator, operatorErr := inspector.LookupOperator(manifest.Operator.UID)
		if operatorErr != nil || operator != manifest.Operator {
			add("operator.identity", Drifted, "directory identity mismatch", "exact manifested UID/name/home", "inspect operator account state manually")
		}
		group, groupErr := inspector.LookupGroup(OperatorGroupName)
		if groupErr != nil || group.ID != manifest.Group.ID || group.Name != manifest.Group.Name || fmt.Sprint(group.Members) != fmt.Sprint(manifest.Group.Members) {
			add("group.identity", Drifted, "directory group mismatch", "exact manifested group ID/name/membership", "inspect directory-service state manually")
		} else if member, err := inspector.IsMember(operator, group); err != nil || !member {
			add("group.membership", Drifted, "operator is not a directory member", "exact manifested membership", "inspect directory-service state manually")
		}
		effective, err := inspector.EffectiveGroups()
		if err != nil || !containsInt(effective, manifest.Group.ID) {
			add("group.not-effective", Drifted, "group absent from current process", fmt.Sprintf("supplementary gid %d", manifest.Group.ID), "refresh the login session, then rerun doctor")
		}
	}

	if !canonicalAbsolute(request.TartHome) {
		add("tart-home.path", Drifted, "noncanonical configured path", "canonical absolute private directory", "correct version-2 host configuration")
	} else if fact, err := inspector.InspectPath(request.TartHome); err != nil || !fact.Exists {
		add("tart-home.missing", Missing, "missing or unreadable", "private operator directory", "create and explicitly initialize tart_home")
	} else if !fact.Directory || fact.Mode&0o077 != 0 || fact.ExtendedACL || (manifest.Version == 1 && fact.UID != manifest.Operator.UID) {
		add("tart-home.unsafe", Drifted, "unsafe type, owner, mode, or ACL", "operator-owned private directory without ACL", "inspect tart_home manually")
	}

	for _, tool := range []struct{ code, path string }{{"ssh", "/usr/bin/ssh"}, {"ssh-keygen", "/usr/bin/ssh-keygen"}} {
		checkTool(inspector, &report, tool.code, tool.path, "", 0, -1, -1)
	}
	checkTool(inspector, &report, "screen", ScreenPath, ScreenExecutableSHA256, 0o755, 0, 0)
	if output, err := inspector.CommandOutput(ScreenPath, "--version"); err != nil || strings.TrimSpace(output) != ScreenVersionOutput {
		add("screen.version", Drifted, "version did not match", ScreenVersionOutput, "use the exact qualified system Screen")
	}
	homebrew, err := inspector.HomebrewSoftnet()
	if err != nil {
		add("homebrew.scan", Drifted, "scan unavailable", "complete read-only mutable Homebrew scan", "inspect mutable Homebrew privilege state manually")
	} else {
		for _, target := range homebrew {
			if target.Privilege != "" {
				add("homebrew.softnet-privileged", Drifted, target.Privilege+" at "+target.Path, "no privileged mutable Homebrew Softnet", "perform attended manual inspection and remediation")
				break
			}
		}
	}
	report.Normalize()
	return report
}

func checkTool(inspector DoctorInspector, report *Report, code, path, digest string, mode uint32, uid, gid int) (PathFact, bool) {
	fact, err := inspector.InspectPath(path)
	if err != nil {
		observed := "path safety inspection unavailable"
		if fact.Exists {
			observed = "path exists but safety inspection failed"
		}
		report.Findings = append(report.Findings, Finding{Code: code + ".metadata", Category: Drifted, Observed: observed, Expected: "single-link regular file without ACL", Remedy: "inspect host tool state manually"})
		return fact, false
	}
	if !fact.Exists {
		report.Findings = append(report.Findings, Finding{Code: code + ".missing", Category: Missing, Observed: "missing or unreadable", Expected: "exact direct regular file", Remedy: "run explicit attended init or install the qualified system tool"})
		return PathFact{}, false
	}
	if !fact.Regular || fact.Links != 1 || fact.ExtendedACL {
		report.Findings = append(report.Findings, Finding{Code: code + ".metadata", Category: Drifted, Observed: "unsafe type, link count, or ACL", Expected: "single-link regular file without ACL", Remedy: "inspect host tool state manually"})
		return fact, false
	}
	if mode != 0 && fact.Mode != mode {
		report.Findings = append(report.Findings, Finding{Code: code + ".mode", Category: Drifted, Observed: fmt.Sprintf("%04o", fact.Mode), Expected: fmt.Sprintf("%04o", mode), Remedy: "inspect host tool state manually"})
	}
	if uid >= 0 && fact.UID != uid || gid >= 0 && fact.GID != gid {
		report.Findings = append(report.Findings, Finding{Code: code + ".owner", Category: Drifted, Observed: fmt.Sprintf("uid=%d gid=%d", fact.UID, fact.GID), Expected: fmt.Sprintf("uid=%d gid=%d", uid, gid), Remedy: "inspect host tool ownership manually"})
	}
	if digest != "" && fact.SHA256 != digest {
		report.Findings = append(report.Findings, Finding{Code: code + ".digest", Category: Drifted, Observed: "digest mismatch", Expected: digest, Remedy: "install the exact qualified executable"})
	}
	return fact, true
}

func trustedSoftnetDirectories() []string {
	digest := filepath.Dir(QualifiedSoftnetPath)
	return []string{"/Library", "/Library/Boxwarden", "/Library/Boxwarden/toolchains", "/Library/Boxwarden/toolchains/softnet", "/Library/Boxwarden/toolchains/softnet/" + SoftnetVersion, digest}
}
func stablePathCode(path string) string {
	return strings.ReplaceAll(strings.Trim(path, "/"), "/", ".")
}
func publicValue(value string) string {
	if value == "" {
		return "unavailable"
	}
	return value
}
func containsInt(values []int, target int) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func productionToolchainPath() string { return "/Library/Boxwarden" }
