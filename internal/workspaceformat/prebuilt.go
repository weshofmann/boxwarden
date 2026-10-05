package workspaceformat

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/weshofmann/boxwarden/internal/execx"
)

type supportManifest struct {
	Version            int               `json:"version"`
	SourceCommit       string            `json:"source_commit"`
	ISOSHA256          string            `json:"iso_sha256"`
	CheckerDebSHA256   string            `json:"checker_deb_sha256"`
	Files              map[string]string `json:"files"`
	SourceInputs       map[string]string `json:"source_inputs"`
	RunnerEntitlements map[string]bool   `json:"runner_entitlements"`
}

var supportSourceSelectors = []string{"go.mod", "internal", "tools/alpha-formatter", "tools/alpha-inspector", "tools/private-beta/build_support.sh", "tools/private-beta/prepare_support.sh", "tools/private-beta/support_resources.py"}

// CheckPrebuiltSupport admits reusable build-time assets, not a formatting
// target. A per-setup seven-file formatter bundle and its private runtime
// binding must independently pass VZFormatter admission before attachment.
func CheckPrebuiltSupport(ctx context.Context, sourceRoot, resourcesRoot string) (string, error) {
	if !filepath.IsAbs(resourcesRoot) || filepath.Clean(resourcesRoot) != resourcesRoot || strings.ContainsAny(resourcesRoot, "\r\n\x00") {
		return "", fmt.Errorf("prebuilt support path must be clean and absolute")
	}
	commit, err := cleanSourceCommit(ctx, sourceRoot)
	if err != nil {
		return "", err
	}
	info, err := os.Lstat(resourcesRoot)
	if err != nil {
		return "", err
	}
	if err := privateDirectory(info); err != nil {
		return "", err
	}
	if err := checkPrivateACL(resourcesRoot, info); err != nil {
		return "", err
	}
	raw, fingerprint, err := bundleFile(filepath.Join(resourcesRoot, "manifest.json"), 0600, 256<<10)
	if err != nil {
		return "", err
	}
	if err := rejectDuplicateJSON(raw); err != nil {
		return "", err
	}
	var m supportManifest
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&m); err != nil {
		return "", err
	}
	if decoder.Decode(new(any)) != io.EOF {
		return "", fmt.Errorf("prebuilt manifest trailing data")
	}
	pins := productionFormatterPins()
	if m.Version != 1 || m.SourceCommit != commit || m.ISOSHA256 != pins.iso || m.CheckerDebSHA256 != pins.deb || len(m.RunnerEntitlements) != 1 || !m.RunnerEntitlements["com.apple.security.virtualization"] {
		return "", fmt.Errorf("prebuilt support provenance differs")
	}
	limits := map[string]struct {
		mode os.FileMode
		max  int64
	}{
		"formatter/kernel-image": {0600, 128 << 20}, "formatter/formatter-initrd": {0600, 256 << 20}, "formatter/alpha-formatter": {0700, 64 << 20}, "formatter/e2fsck.static": {0600, 16 << 20}, "formatter/alpha-formatter-host": {0700, 64 << 20}, "formatter/binding.swift": {0600, 4096},
		"inspector/casper/vmlinuz": {0400, 128 << 20}, "inspector/casper/initrd": {0400, 256 << 20}, "inspector/kernel-image": {0600, 128 << 20}, "inspector/alpha-probe": {0700, 16 << 20}, "inspector/alpha-inspector": {0700, 64 << 20}}
	if len(m.Files) != len(limits) || m.Files["formatter/kernel-image"] != pins.kernel || m.Files["inspector/kernel-image"] != pins.kernel || m.Files["formatter/e2fsck.static"] != pins.checker || m.Files["inspector/casper/vmlinuz"] != "000d59171b8e49f31f55c0d52123571ca8963718220fa8bacee1d65fdcbad617" {
		return "", fmt.Errorf("prebuilt artifact inventory or fixed pins differ")
	}
	for _, dir := range []string{"formatter", "inspector", "inspector/casper"} {
		path := filepath.Join(resourcesRoot, dir)
		info, err := os.Lstat(path)
		if err != nil {
			return "", err
		}
		if err := privateDirectory(info); err != nil {
			return "", err
		}
		if err := checkPrivateACL(path, info); err != nil {
			return "", err
		}
	}
	for name, limit := range limits {
		contents, digest, err := bundleFile(filepath.Join(resourcesRoot, name), limit.mode, limit.max)
		if err != nil {
			return "", err
		}
		if !lowerHex(m.Files[name], 64) || digest != m.Files[name] {
			return "", fmt.Errorf("prebuilt artifact %s digest differs", name)
		}
		if name == "formatter/binding.swift" && string(contents) != runtimeBindingSource {
			return "", fmt.Errorf("prebuilt binding source differs")
		}
	}
	runner := execx.OSRunner{MaxOutputBytes: 256 << 10, MaxStdinBytes: 8192}
	args := append([]string{"-C", sourceRoot, "ls-files", "-z", "--"}, supportSourceSelectors...)
	listing, err := runner.Run(ctx, execx.Command{Path: "/usr/bin/git", Args: args, Env: []string{"PATH=/usr/bin:/bin", "LANG=C", "LC_ALL=C"}})
	if err != nil || listing.Truncated {
		return "", fmt.Errorf("prebuilt source inventory: %v", err)
	}
	names := strings.Split(strings.TrimSuffix(listing.Stdout, "\x00"), "\x00")
	if len(names) == 0 || len(names) != len(m.SourceInputs) {
		return "", fmt.Errorf("prebuilt source inventory differs")
	}
	for _, name := range names {
		_, digest, err := hashSupportSource(filepath.Join(sourceRoot, name))
		if err != nil || digest != m.SourceInputs[name] {
			return "", fmt.Errorf("prebuilt source %s differs: %v", name, err)
		}
	}
	for _, name := range []string{"formatter/alpha-formatter-host", "inspector/alpha-inspector"} {
		path := filepath.Join(resourcesRoot, name)
		if _, err := checkVZCommand(ctx, runner, execx.Command{Path: "/usr/bin/codesign", Args: []string{"--verify", "--strict", path}}); err != nil {
			return "", err
		}
		result, err := checkVZCommand(ctx, runner, execx.Command{Path: "/usr/bin/codesign", Args: []string{"-d", "--entitlements", ":-", path}})
		if err != nil {
			return "", err
		}
		converted, err := checkVZCommand(ctx, runner, execx.Command{Path: "/usr/bin/plutil", Args: []string{"-convert", "json", "-o", "-", "-"}, Stdin: []byte(result.Stdout)})
		if err != nil {
			return "", err
		}
		var actual map[string]bool
		if rejectDuplicateJSON([]byte(converted.Stdout)) != nil || json.Unmarshal([]byte(converted.Stdout), &actual) != nil || len(actual) != 1 || !actual["com.apple.security.virtualization"] {
			return "", fmt.Errorf("prebuilt helper entitlement differs")
		}
	}
	return fingerprint, nil
}

// Source files keep their tracked executable/read modes; they are admitted by
// exact clean checkout and digest rather than private artifact permissions.
func hashSupportSource(path string) ([]byte, string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, "", err
	}
	if !info.Mode().IsRegular() {
		return nil, "", fmt.Errorf("support source is not regular")
	}
	return bundleFile(path, info.Mode().Perm(), 4<<20)
}
