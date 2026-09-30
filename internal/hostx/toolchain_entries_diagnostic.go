//go:build n1diagnostic && !n1candidate

package hostx

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

const emptyFileSHA256 = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

func stagedToolchainEntries() string   { return "[launch.lock softnet]" }
func completeToolchainEntries() string { return "[launch.lock manifest.json softnet]" }

func (p RootedPublisher) stageLaunchLock(stage string, g Group) error {
	if err := p.checkpoint("before-launch-lock"); err != nil {
		return err
	}
	path := filepath.Join(stage, "launch.lock")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	err = f.Sync()
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err = p.changeOwner(path, p.expectedRootUID(), g.ID); err != nil {
		return err
	}
	if err = chmodUnix(path, 0o440); err != nil {
		return err
	}
	if err = p.validateLaunchLock(stage, g); err != nil {
		return err
	}
	return p.checkpoint("after-launch-lock")
}

func (p RootedPublisher) validateLaunchLock(dir string, g Group) error {
	path := filepath.Join(dir, "launch.lock")
	if err := p.validateFile(path, p.expectedRootUID(), g.ID, 0o440, emptyFileSHA256); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil || info.Size() != 0 {
		return fmt.Errorf("launch.lock must be zero bytes")
	}
	return nil
}

type selectedDirectoryInspector interface {
	DirectoryEntries(string) ([]string, error)
}

func inspectSelectedTree(inspector DoctorInspector, m Manifest, report *Report) {
	reject := func(code string) {
		report.Findings = append(report.Findings, Finding{Code: code, Category: Drifted, Observed: "diagnostic tree is incomplete or unsafe", Expected: "exact protected three-file tree with zero-byte launch.lock", Remedy: "inspect the installed diagnostic tree manually"})
	}
	reader, ok := inspector.(selectedDirectoryInspector)
	if !ok {
		reject("softnet.entries")
		return
	}
	dir := filepath.Dir(QualifiedSoftnetPath)
	names, err := reader.DirectoryEntries(dir)
	sort.Strings(names)
	if err != nil || fmt.Sprint(names) != "[launch.lock manifest.json softnet]" {
		reject("softnet.entries")
	}
	lock, err := inspector.InspectPath(filepath.Join(dir, "launch.lock"))
	if err != nil || !exactToolFact(lock, emptyFileSHA256, 0o440, 0, m.Group.ID) {
		reject("softnet.launch-lock")
	}
}

// ErrDiagnosticLegacyUninstall refuses ordinary recursive cleanup permanently.
// Qualification-only exact admission/EX/census/unlink is a separate Task4b API.
var ErrDiagnosticLegacyUninstall = errors.New("diagnostic tree requires dedicated qualification cleanup")

func refuseSelectedLegacyUninstall() error { return ErrDiagnosticLegacyUninstall }
