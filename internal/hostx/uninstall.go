package hostx

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
)

// ConsumerChecker observes both live and recorded supervisor ownership. An
// error is deliberately a refusal: uninstall may not guess that a digest tree
// is unused.
type ConsumerChecker interface {
	HasConsumer(context.Context, string) (bool, error)
}

func CheckUninstallable(ctx context.Context, digest string, consumers ConsumerChecker) error {
	if digest != SoftnetExecutableSHA256 {
		return fmt.Errorf("uninstall requires the exact manifested Softnet digest")
	}
	if consumers == nil {
		return fmt.Errorf("uninstall requires a verifiable consumer inventory")
	}
	active, err := consumers.HasConsumer(ctx, digest)
	if err != nil {
		return fmt.Errorf("refuse uninstall: cannot verify consumers: %w", err)
	}
	if active {
		return fmt.Errorf("refuse uninstall: a live or recorded supervisor uses digest %s", digest)
	}
	return nil
}

// RootedUninstaller removes one exact, fully validated digest tree. Publisher
// supplies the same rooted validation policy used by installation; Consumers
// must cover both recorded and live supervisor ownership.
type RootedUninstaller struct {
	Publisher RootedPublisher
	Consumers ConsumerChecker

	syncParent func(string) error
}

func (u RootedUninstaller) Uninstall(ctx context.Context, digest string, caller Caller, group Group) error {
	if digest != SoftnetExecutableSHA256 {
		return fmt.Errorf("uninstall requires the exact full manifested Softnet digest")
	}
	if u.Consumers == nil {
		return fmt.Errorf("uninstall requires a verifiable consumer inventory")
	}
	first, err := u.validateExactTree(caller, group, digest)
	if err != nil {
		return fmt.Errorf("refuse uninstall of unsafe digest tree: %w", err)
	}
	if err := CheckUninstallable(ctx, digest, u.Consumers); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	second, err := u.validateExactTree(caller, group, digest)
	if err != nil {
		return fmt.Errorf("refuse uninstall after digest tree changed: %w", err)
	}
	if first != second {
		return fmt.Errorf("refuse uninstall after digest tree identity changed")
	}
	target := u.Publisher.finalDir()
	if filepath.Base(target) != digest || !pathWithin(u.Publisher.Root, target) {
		return fmt.Errorf("refuse uninstall target outside exact rooted digest")
	}
	if err := removeOwnedTree(target, second, u.Publisher.expectedRootUID()); err != nil {
		return fmt.Errorf("remove exact validated digest tree: %w", err)
	}
	syncParent := u.syncParent
	if syncParent == nil {
		syncParent = syncDirectory
	}
	if err := syncParent(filepath.Dir(target)); err != nil {
		return fmt.Errorf("fsync digest parent after exact uninstall: %w", err)
	}
	return nil
}

func (u RootedUninstaller) validateExactTree(caller Caller, group Group, digest string) (fileIdentity, error) {
	publisher := u.Publisher
	if filepath.Base(publisher.finalDir()) != digest {
		return fileIdentity{}, fmt.Errorf("resolved digest directory does not match requested digest")
	}
	if err := publisher.validateRootParent(); err != nil {
		return fileIdentity{}, err
	}
	if err := publisher.validateNoCurrentPointer(); err != nil {
		return fileIdentity{}, err
	}
	if err := publisher.validateInstalledDirectoryChain(); err != nil {
		return fileIdentity{}, err
	}
	manifest, err := publisher.readInstalledManifest()
	if err != nil {
		return fileIdentity{}, err
	}
	if manifest.Softnet.ExecutableSHA256 != digest || filepath.Base(filepath.Dir(manifest.Softnet.Path)) != digest {
		return fileIdentity{}, fmt.Errorf("manifest does not select requested digest directory")
	}
	if manifest.Operator != (Operator{UID: caller.UID, Name: caller.Name, Home: caller.Home}) || !sameGroup(manifest.Group, group) {
		return fileIdentity{}, fmt.Errorf("manifest does not bind exact caller and group")
	}
	request := InstallRequest{Version: InstallRequestVersion, Tart: manifest.Tart, TartHome: manifest.TartHome}
	if err := publisher.validatePairedInputs(request, caller); err != nil {
		return fileIdentity{}, err
	}
	if err := publisher.validateCompleteTree(request, caller, group); err != nil {
		return fileIdentity{}, err
	}
	return statIdentity(publisher.finalDir())
}

func (p RootedPublisher) readInstalledManifest() (Manifest, error) {
	path := filepath.Join(p.finalDir(), "manifest.json")
	if err := p.validateFile(path, p.expectedRootUID(), p.expectedRootGID(), manifestMode, ""); err != nil {
		return Manifest{}, err
	}
	file, err := openVerifiedRegular(path, "", false)
	if err != nil {
		return Manifest{}, err
	}
	data, readErr := io.ReadAll(io.LimitReader(file, maxManifestBytes+1))
	closeErr := file.Close()
	if readErr != nil || closeErr != nil || len(data) > maxManifestBytes {
		return Manifest{}, fmt.Errorf("read bounded installed manifest")
	}
	return ParseManifest(data)
}

func sameGroup(a, b Group) bool {
	if a.ID != b.ID || a.Name != b.Name || len(a.Members) != len(b.Members) {
		return false
	}
	for index := range a.Members {
		if a.Members[index] != b.Members[index] {
			return false
		}
	}
	return true
}
