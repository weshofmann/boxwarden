//go:build n1diagnostic && n1cleanup && !n1candidate && (darwin || linux)

package hostx

import (
	"context"
	"fmt"
	"github.com/weshofmann/boxwarden/internal/execx"
	"os"
	"path/filepath"
	"sort"
)

// CheckDiagnosticPreflight only observes the fixed stock host/operator policy.
// No cleanup admission/lock, enrollment, install or dispatch capability is used.
func (s SystemDoctor) CheckDiagnosticPreflight(ctx context.Context, r Request) error {
	_, _, e := s.diagnosticPreflightAdmissionIDs(ctx, r, os.Getuid(), os.Geteuid())
	return e
}
func (s SystemDoctor) diagnosticPreflightAdmission(ctx context.Context, r Request) (DoctorInspector, Manifest, error) {
	return s.diagnosticAdmissionIDs(ctx, r, os.Getuid(), os.Geteuid(), false)
}
func (s SystemDoctor) diagnosticPreflightAdmissionIDs(ctx context.Context, r Request, uid, euid int) (DoctorInspector, Manifest, error) {
	return s.diagnosticAdmissionIDs(ctx, r, uid, euid, true)
}
func (s SystemDoctor) diagnosticAdmissionIDs(ctx context.Context, r Request, uid, euid int, stock bool) (DoctorInspector, Manifest, error) {
	if uid != 0 || euid != 0 {
		return nil, Manifest{}, ErrDiagnosticCleanup
	}
	if len(r.ConfiguredStateRoots) != 1 || r.ConfiguredStateRoots[0] != "/Volumes/BoxwardenAlphaQualification/n1-diagnostic-20260930-state" || r.TartPath != "/Users/devel/Library/Application Support/boxwarden/toolchains/tart/2.32.1/tart.app/Contents/MacOS/tart" || r.TartHome != "/Users/devel/Library/Application Support/boxwarden/tart" || r.SoftnetPath != "/Users/devel/Backup/boxwarden_archive/n1-diagnostic-20260930/package/artifacts/softnet-diagnostic" {
		return nil, Manifest{}, ErrDiagnosticCleanup
	}
	inspector := s.inspector
	if inspector == nil {
		n := NewOSDoctorInspector().(*osDoctorInspector)
		op, e := n.LookupOperator(501)
		if e != nil || op != (Operator{501, "devel", "/Users/devel"}) {
			return nil, Manifest{}, ErrDiagnosticCleanup
		}
		n.policyOperator = "devel"
		n.runner = execx.OSRunner{MaxOutputBytes: 16 << 10, StrictStderr: true}
		inspector = n
	}
	s.inspector = inspector
	if stock && inspector.Platform() != (PlatformFact{OS: QualifiedPlatform, Arch: QualifiedArch, Release: TrialMacOS, Build: TrialMacOSBuild}) {
		return nil, Manifest{}, ErrDiagnosticCleanup
	}
	var a doctorInspection
	if stock {
		a = s.inspectSoftnetPolicy(ctx, r, false, stockPreflightPolicy())
	} else {
		a = s.inspectPolicy(ctx, r, false)
	}
	m := a.manifest
	if a.report.Status != Healthy || m.Operator != (Operator{501, "devel", "/Users/devel"}) || m.Group.ID != 501 || m.Group.Name != OperatorGroupName || len(m.Group.Members) != 1 || m.Group.Members[0] != 501 || ctx.Err() != nil {
		return nil, Manifest{}, ErrDiagnosticCleanup
	}
	return inspector, m, nil
}

func stockPreflightPolicy() softnetInspectionPolicy {
	const p = "/Library/Boxwarden/toolchains/softnet/0.19.0/ab333619fc8bd7277837545e49a771baa994c01c3e8c14904ae4cc4c1f37269e/softnet"
	expected := ToolIdentity{p, "0.19.0", "ab333619fc8bd7277837545e49a771baa994c01c3e8c14904ae4cc4c1f37269e", "1612e1296834aae0b6389650c7c5190add1ee8d71474e328691e67679ecda53c"}
	return softnetInspectionPolicy{p, expected.ExecutableSHA256, func(raw []byte) (Manifest, error) {
		return parseManifestSoftnet(raw, func(x ToolIdentity) bool { return x == expected })
	}, func(i DoctorInspector, m Manifest, r *Report) {
		reader, ok := i.(interface {
			DirectoryEntries(string) ([]string, error)
		})
		if !ok {
			r.Findings = append(r.Findings, Finding{Code: "stock.entries", Category: Drifted})
			return
		}
		names, e := reader.DirectoryEntries(filepath.Dir(p))
		sort.Strings(names)
		if e != nil || fmt.Sprint(names) != "[manifest.json softnet]" {
			r.Findings = append(r.Findings, Finding{Code: "stock.entries", Category: Drifted})
		}
	}}
}
