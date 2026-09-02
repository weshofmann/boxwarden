package hostx

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	trustedDirectoryMode = 0o755
	manifestMode         = 0o400
)

// ACLInspector reports whether a path carries an extended ACL. It is read-only.
type ACLInspector interface {
	HasExtendedACL(string) (bool, error)
}

// RootedPublisher realizes the privileged, two-step publication state machine.
// Root is injectable solely for deterministic synthetic-root tests.
type RootedPublisher struct {
	Root string
	Now  func() time.Time
	ACL  ACLInspector

	rootUID, rootGID int
	softnetDigest    string
	tartDigest       string
	softnetMode      uint32
	chown            func(string, int, int) error
	acl              ACLInspector
	token            func() string
	fail             func(string) error
	observe          func(string)
}

func (p RootedPublisher) finalDir() string {
	return filepath.Join(p.Root, "toolchains", "softnet", SoftnetVersion, SoftnetExecutableSHA256)
}
func (p RootedPublisher) expectedRootUID() int { return p.rootUID }
func (p RootedPublisher) expectedRootGID() int { return p.rootGID }
func (p RootedPublisher) expectedDigest() string {
	if p.softnetDigest != "" {
		return p.softnetDigest
	}
	return SoftnetExecutableSHA256
}
func (p RootedPublisher) expectedSoftnetMode() uint32 {
	if p.softnetMode != 0 {
		return p.softnetMode
	}
	return SoftnetMode
}
func (p RootedPublisher) expectedTartDigest() string {
	if p.tartDigest != "" {
		return p.tartDigest
	}
	return TartExecutableSHA256
}
func (p RootedPublisher) aclInspector() ACLInspector {
	if p.acl != nil {
		return p.acl
	}
	if p.ACL != nil {
		return p.ACL
	}
	return OSACLInspector{}
}
func (p RootedPublisher) changeOwner(path string, uid, gid int) error {
	if p.chown != nil {
		return p.chown(path, uid, gid)
	}
	return os.Chown(path, uid, gid)
}
func (p RootedPublisher) stageToken() string {
	if p.token != nil {
		return p.token()
	}
	return fmt.Sprintf("%d-%d", os.Getpid(), time.Now().UnixNano())
}
func (p RootedPublisher) checkpoint(step string) error {
	if p.fail != nil {
		return p.fail(step)
	}
	return nil
}
func (p RootedPublisher) observed(step string) {
	if p.observe != nil {
		p.observe(step)
	}
}

func (p RootedPublisher) State(_ context.Context, r InstallRequest, c Caller, g Group) (publicationState, error) {
	if err := p.validateRootParent(); err != nil {
		return publicationUnexpected, err
	}
	if err := p.validateNoCurrentPointer(); err != nil {
		return publicationUnexpected, err
	}
	if err := p.validatePairedInputs(r, c); err != nil {
		return publicationUnexpected, err
	}
	info, err := os.Lstat(p.finalDir())
	if os.IsNotExist(err) {
		return publicationAbsent, nil
	}
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return publicationUnexpected, nil
	}
	if err := p.validateCompleteTree(r, c, g); err != nil {
		return publicationUnexpected, nil
	}
	return publicationComplete, nil
}

func (p RootedPublisher) Preflight(_ context.Context, r InstallRequest, c Caller) (publicationState, error) {
	if err := p.validateRootParent(); err != nil {
		return publicationUnexpected, err
	}
	if err := p.validateNoCurrentPointer(); err != nil {
		return publicationUnexpected, err
	}
	if err := p.validatePairedInputs(r, c); err != nil {
		return publicationUnexpected, err
	}
	if _, err := os.Lstat(p.finalDir()); os.IsNotExist(err) {
		parent := filepath.Dir(p.finalDir())
		if entries, readErr := os.ReadDir(parent); readErr == nil {
			prefix := "." + SoftnetExecutableSHA256 + ".staging-"
			for _, entry := range entries {
				if strings.HasPrefix(entry.Name(), prefix) {
					return publicationUnexpected, nil
				}
			}
		} else if !os.IsNotExist(readErr) {
			return publicationUnexpected, readErr
		}
		return publicationAbsent, nil
	} else if err != nil {
		return publicationUnexpected, err
	}
	manifestPath := filepath.Join(p.finalDir(), "manifest.json")
	file, err := openVerifiedRegular(manifestPath, "", false)
	if err != nil {
		return publicationUnexpected, nil
	}
	data, readErr := io.ReadAll(io.LimitReader(file, maxManifestBytes+1))
	file.Close()
	if readErr != nil || len(data) > maxManifestBytes {
		return publicationUnexpected, nil
	}
	manifest, err := ParseManifest(data)
	if err != nil || manifest.Operator != (Operator{UID: c.UID, Name: c.Name, Home: c.Home}) {
		return publicationUnexpected, nil
	}
	if err := p.validateCompleteTree(r, c, manifest.Group); err != nil {
		return publicationUnexpected, nil
	}
	return publicationComplete, nil
}

func (p RootedPublisher) Publish(ctx context.Context, r InstallRequest, c Caller, g Group) error {
	if err := r.Validate(); err != nil {
		return err
	}
	if err := p.validateRootParent(); err != nil {
		return err
	}
	if err := p.validateNoCurrentPointer(); err != nil {
		return err
	}
	if err := p.validatePairedInputs(r, c); err != nil {
		return err
	}
	if pathsOverlap(p.Root, r.SoftnetSource) || pathsOverlap(p.Root, r.Tart.Path) || pathsOverlap(p.Root, r.TartHome) || pathsOverlap(r.SoftnetSource, r.Tart.Path) || pathsOverlap(r.SoftnetSource, r.TartHome) || pathsOverlap(r.Tart.Path, r.TartHome) {
		return fmt.Errorf("trusted input path overlaps installed toolchain root")
	}
	source, err := openVerifiedRegular(r.SoftnetSource, p.expectedDigest(), true)
	if err != nil {
		return fmt.Errorf("reopen qualified Softnet source: %w", err)
	}
	defer source.Close()
	if err := p.checkpoint("after-source-open"); err != nil {
		return err
	}

	parent := filepath.Dir(p.finalDir())
	if err := p.ensureDirectoryChain(parent); err != nil {
		return err
	}
	if _, err := os.Lstat(p.finalDir()); err == nil {
		return fmt.Errorf("refuse existing final digest tree")
	} else if !os.IsNotExist(err) {
		return err
	}
	token := p.stageToken()
	if token == "" || filepath.Base(token) != token || token == "." || token == ".." {
		return fmt.Errorf("invalid publication staging token")
	}
	stage := filepath.Join(parent, "."+SoftnetExecutableSHA256+".staging-"+token)
	if err := os.Mkdir(stage, 0o700); err != nil {
		return fmt.Errorf("create owned staging directory: %w", err)
	}
	if err := p.changeOwner(stage, p.expectedRootUID(), p.expectedRootGID()); err != nil {
		_ = os.Remove(stage)
		return fmt.Errorf("own staging directory: %w", err)
	}
	stageIdentity, err := statIdentity(stage)
	if err != nil {
		_ = os.Remove(stage)
		return err
	}
	stageOwned := true
	defer func() {
		if stageOwned {
			_ = removeOwnedTree(stage, stageIdentity, p.expectedRootUID())
		}
	}()
	if err := ctx.Err(); err != nil {
		return err
	}

	softnetStage := filepath.Join(stage, "softnet")
	destination, err := os.OpenFile(softnetStage, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(destination, source)
	if copyErr == nil {
		copyErr = destination.Sync()
	}
	if closeErr := destination.Close(); copyErr == nil {
		copyErr = closeErr
	}
	if copyErr != nil {
		return fmt.Errorf("copy and fsync Softnet: %w", copyErr)
	}
	if err := p.checkpoint("after-copy"); err != nil {
		return err
	}
	if err := p.changeOwner(softnetStage, p.expectedRootUID(), g.ID); err != nil {
		return err
	}
	if err := chmodUnix(softnetStage, p.expectedSoftnetMode()); err != nil {
		return err
	}
	if err := p.validateFile(softnetStage, p.expectedRootUID(), g.ID, os.FileMode(p.expectedSoftnetMode()), p.expectedDigest()); err != nil {
		return fmt.Errorf("validate staged Softnet: %w", err)
	}
	if err := os.Chmod(stage, trustedDirectoryMode); err != nil {
		return err
	}
	if err := p.validateDirectory(stage, trustedDirectoryMode); err != nil {
		return fmt.Errorf("validate staging directory: %w", err)
	}
	entries, err := os.ReadDir(stage)
	if err != nil || len(entries) != 1 || entries[0].Name() != "softnet" {
		return fmt.Errorf("staging directory contains unexpected entries")
	}
	if err := syncDirectory(stage); err != nil {
		return fmt.Errorf("fsync complete staging directory: %w", err)
	}
	if err := p.checkpoint("before-tree-rename"); err != nil {
		return err
	}
	if err := renameExclusive(stage, p.finalDir()); err != nil {
		return fmt.Errorf("publish digest directory without replacement: %w", err)
	}
	stageOwned = false
	p.observed("tree-renamed")
	if err := syncDirectory(parent); err != nil {
		return fmt.Errorf("fsync digest parent: %w", err)
	}
	p.observed("tree-synced")
	return p.publishManifest(r, c, g, token)
}

func (p RootedPublisher) publishManifest(r InstallRequest, c Caller, g Group, token string) error {
	now := time.Now().UTC()
	if p.Now != nil {
		now = p.Now().UTC()
	}
	m := Manifest{
		Version: 1, Platform: QualifiedPlatform, MacOS: QualifiedMacOS, Tart: r.Tart,
		Softnet: ToolIdentity{Path: QualifiedSoftnetPath, Version: SoftnetVersion, ExecutableSHA256: SoftnetExecutableSHA256, ArchiveSHA256: SoftnetArchiveSHA256},
		RootUID: 0, Group: g, Operator: Operator{UID: c.UID, Name: c.Name, Home: c.Home},
		TartHome: r.TartHome, SoftnetMode: SoftnetMode, InstalledAt: now,
	}
	data, err := json.Marshal(m)
	if err != nil {
		return err
	}
	temporary := filepath.Join(p.finalDir(), ".manifest-staging-"+token)
	final := filepath.Join(p.finalDir(), "manifest.json")
	file, err := os.OpenFile(temporary, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("create manifest staging file: %w", err)
	}
	created, statErr := file.Stat()
	if statErr != nil {
		file.Close()
		return statErr
	}
	defer func() {
		if current, err := os.Lstat(temporary); err == nil && sameFileInfo(current, created) {
			_ = os.Remove(temporary)
		}
	}()
	if _, err = file.Write(data); err == nil {
		err = file.Sync()
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return fmt.Errorf("write and fsync manifest: %w", err)
	}
	if err := p.changeOwner(temporary, p.expectedRootUID(), p.expectedRootGID()); err != nil {
		return err
	}
	if err := chmodUnix(temporary, manifestMode); err != nil {
		return err
	}
	if err := p.validateFile(temporary, p.expectedRootUID(), p.expectedRootGID(), manifestMode, ""); err != nil {
		return fmt.Errorf("validate staged manifest: %w", err)
	}
	if err := p.checkpoint("before-manifest-rename"); err != nil {
		return err
	}
	if err := renameExclusive(temporary, final); err != nil {
		return fmt.Errorf("publish manifest without replacement: %w", err)
	}
	p.observed("manifest-renamed")
	if err := syncDirectory(p.finalDir()); err != nil {
		return fmt.Errorf("fsync published manifest directory: %w", err)
	}
	p.observed("manifest-synced")
	return nil
}

func (p RootedPublisher) validateCompleteTree(r InstallRequest, c Caller, g Group) error {
	if err := p.validateInstalledDirectoryChain(); err != nil {
		return err
	}
	entries, err := os.ReadDir(p.finalDir())
	if err != nil {
		return err
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	if fmt.Sprint(names) != "[manifest.json softnet]" {
		return fmt.Errorf("digest directory contains unexpected entries")
	}
	if err := p.validateDirectory(p.finalDir(), trustedDirectoryMode); err != nil {
		return err
	}
	if err := p.validateFile(filepath.Join(p.finalDir(), "softnet"), p.expectedRootUID(), g.ID, os.FileMode(p.expectedSoftnetMode()), p.expectedDigest()); err != nil {
		return err
	}
	manifestPath := filepath.Join(p.finalDir(), "manifest.json")
	if err := p.validateFile(manifestPath, p.expectedRootUID(), p.expectedRootGID(), manifestMode, ""); err != nil {
		return err
	}
	manifestFile, err := openVerifiedRegular(manifestPath, "", false)
	if err != nil {
		return err
	}
	data, readErr := io.ReadAll(io.LimitReader(manifestFile, maxManifestBytes+1))
	closeErr := manifestFile.Close()
	if readErr != nil || closeErr != nil || len(data) > maxManifestBytes {
		return fmt.Errorf("read bounded manifest")
	}
	m, err := ParseManifest(data)
	if err != nil {
		return err
	}
	if m.Tart != r.Tart || m.TartHome != r.TartHome || m.Operator != (Operator{UID: c.UID, Name: c.Name, Home: c.Home}) || m.Group.ID != g.ID || m.Group.Name != g.Name || fmt.Sprint(m.Group.Members) != fmt.Sprint(g.Members) {
		return fmt.Errorf("manifest does not bind requested caller, group, and Tart identity")
	}
	return nil
}

func (p RootedPublisher) validateInstalledDirectoryChain() error {
	rel, err := filepath.Rel(p.Root, p.finalDir())
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("final digest directory escapes trusted root")
	}
	current := p.Root
	if err := p.validateDirectory(current, trustedDirectoryMode); err != nil {
		return err
	}
	for _, component := range strings.Split(rel, string(filepath.Separator)) {
		current = filepath.Join(current, component)
		if err := p.validateDirectory(current, trustedDirectoryMode); err != nil {
			return err
		}
	}
	return nil
}

func (p RootedPublisher) validateRootParent() error {
	if p.Root == "" || !canonicalAbsolute(p.Root) {
		return fmt.Errorf("rooted publisher requires canonical absolute root")
	}
	parent := filepath.Dir(p.Root)
	info, err := os.Lstat(parent)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o022 != 0 {
		return fmt.Errorf("toolchain root parent is absent, linked, or writable by group/other")
	}
	uid, _, ok := ownership(info)
	if !ok || uid != p.expectedRootUID() {
		return fmt.Errorf("toolchain root parent is not owned by the trusted root identity")
	}
	acl, err := p.aclInspector().HasExtendedACL(parent)
	if err != nil || acl {
		return fmt.Errorf("toolchain root parent has unverifiable or extended ACL")
	}
	return nil
}

func (p RootedPublisher) validateNoCurrentPointer() error {
	current := filepath.Join(p.Root, "toolchains", "softnet", "current")
	if _, err := os.Lstat(current); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return fmt.Errorf("inspect prohibited current pointer: %w", err)
	}
	return fmt.Errorf("prohibited mutable Softnet current pointer exists")
}

func (p RootedPublisher) validatePairedInputs(r InstallRequest, c Caller) error {
	tart, err := openVerifiedRegular(r.Tart.Path, p.expectedTartDigest(), true)
	if err != nil {
		return fmt.Errorf("validate paired Tart executable: %w", err)
	}
	info, statErr := tart.Stat()
	closeErr := tart.Close()
	if statErr != nil || closeErr != nil || unixMode(info)&0o111 == 0 {
		return fmt.Errorf("paired Tart executable metadata is unsafe")
	}
	acl, err := p.aclInspector().HasExtendedACL(r.Tart.Path)
	if err != nil || acl {
		return fmt.Errorf("paired Tart executable ACL is unsafe or unverifiable")
	}
	if _, err := snapshotPath(r.TartHome); err != nil {
		return fmt.Errorf("validate paired tart_home path: %w", err)
	}
	home, err := os.Lstat(r.TartHome)
	if err != nil || !home.IsDir() || home.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("paired tart_home is not a private direct directory")
	}
	uid, _, ok := ownership(home)
	if !ok || uid != c.UID {
		return fmt.Errorf("paired tart_home is not owned by the trusted operator")
	}
	acl, err = p.aclInspector().HasExtendedACL(r.TartHome)
	if err != nil || acl {
		return fmt.Errorf("paired tart_home ACL is unsafe or unverifiable")
	}
	return nil
}

func (p RootedPublisher) ensureDirectoryChain(target string) error {
	rel, err := filepath.Rel(p.Root, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("directory target escapes toolchain root")
	}
	paths := []string{p.Root}
	current := p.Root
	if rel != "." {
		for _, component := range strings.Split(rel, string(filepath.Separator)) {
			current = filepath.Join(current, component)
			paths = append(paths, current)
		}
	}
	for _, path := range paths {
		info, err := os.Lstat(path)
		if os.IsNotExist(err) {
			if err := os.Mkdir(path, 0o700); err != nil {
				return fmt.Errorf("create trusted directory %q: %w", path, err)
			}
			if err := p.changeOwner(path, p.expectedRootUID(), p.expectedRootGID()); err != nil {
				return err
			}
			if err := os.Chmod(path, trustedDirectoryMode); err != nil {
				return err
			}
		} else if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("trusted directory %q is unsafe", path)
		}
		if err := p.validateDirectory(path, 0); err != nil {
			return err
		}
	}
	return nil
}

func (p RootedPublisher) validateDirectory(path string, exactMode os.FileMode) error {
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("trusted path %q is not a direct directory", path)
	}
	uid, gid, ok := ownership(info)
	if !ok || uid != p.expectedRootUID() || gid != p.expectedRootGID() {
		return fmt.Errorf("trusted directory %q has wrong ownership", path)
	}
	if (exactMode != 0 && info.Mode().Perm() != exactMode) || info.Mode().Perm()&0o022 != 0 {
		return fmt.Errorf("trusted directory %q has unsafe mode %04o", path, info.Mode().Perm())
	}
	acl, err := p.aclInspector().HasExtendedACL(path)
	if err != nil || acl {
		return fmt.Errorf("trusted directory %q has unverifiable or extended ACL", path)
	}
	return nil
}

func (p RootedPublisher) validateFile(path string, uid, gid int, mode os.FileMode, digest string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect trusted file %q: %w", path, err)
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || links(info) != 1 || unixMode(info) != uint32(mode) {
		return fmt.Errorf("trusted file %q has unsafe type=%v links=%d mode=%04o; expected regular links=1 mode=%04o", path, info.Mode(), links(info), unixMode(info), mode)
	}
	gotUID, gotGID, ok := ownership(info)
	if !ok || gotUID != uid || gotGID != gid {
		return fmt.Errorf("trusted file %q has wrong ownership", path)
	}
	acl, err := p.aclInspector().HasExtendedACL(path)
	if err != nil || acl {
		return fmt.Errorf("trusted file %q has unverifiable or extended ACL", path)
	}
	if digest != "" {
		opened, err := openVerifiedRegular(path, digest, false)
		if err != nil {
			return err
		}
		return opened.Close()
	}
	return nil
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

func pathWithin(root, candidate string) bool {
	rel, err := filepath.Rel(root, candidate)
	return err == nil && (rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))))
}

func pathsOverlap(a, b string) bool { return pathWithin(a, b) || pathWithin(b, a) }
