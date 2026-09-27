package workspaceformat

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"

	"github.com/weshofmann/boxwarden/internal/domain"
	"github.com/weshofmann/boxwarden/internal/execx"
)

const (
	formatterISOSHA     = "c2610520bf582976839a1724c669e1cfed0547427be5a0ad12d457b92b46ffbe"
	formatterDebSHA     = "0a3d402fd7c7c07f63b8104d84a47378f57022e7c816190e6cc99950492d8cae"
	formatterCheckerSHA = "e5e8f8b30641fab6e3f402c9746ce0537ad95c528e96cde2e446ca0968e63279"
	formatterKernelSHA  = "a1586ff3cb7ced7c40dcb0aba5bf320ebb94a46d1a6505eb03157a8f9525632d"
)

type formatterPins struct{ iso, deb, checker, kernel string }

func productionFormatterPins() formatterPins {
	return formatterPins{formatterISOSHA, formatterDebSHA, formatterCheckerSHA, formatterKernelSHA}
}

type vzBundleDocument struct {
	Version            int               `json:"version"`
	SourceCommit       string            `json:"source_commit"`
	ISOSHA256          string            `json:"iso_sha256"`
	CheckerDebSHA256   string            `json:"checker_deb_sha256"`
	Files              map[string]string `json:"files"`
	ManagedStateRoot   string            `json:"managed_state_root"`
	ManagedDomain      string            `json:"managed_domain"`
	RunnerEntitlements map[string]bool   `json:"runner_entitlements"`
}

type vzBundle struct{ runner, kernel, initrd string }

func managedBindingSource(root string, selected domain.ID) string {
	return "import Foundation\nfunc managedStateRoot() -> String? {\n" +
		"    String(data: Data(base64Encoded: \"" + base64.StdEncoding.EncodeToString([]byte(root)) + "\")!, encoding: .utf8)\n" +
		"}\nfunc managedDomain() -> String? { \"" + string(selected) + "\" }\n"
}

func lowerHex(raw string, n int) bool {
	if len(raw) != n {
		return false
	}
	for i := range raw {
		if raw[i] < '0' || raw[i] > '9' && raw[i] < 'a' || raw[i] > 'f' {
			return false
		}
	}
	return true
}

func bundleFile(path string, mode os.FileMode, maximum int64) ([]byte, string, error) {
	before, err := os.Lstat(path)
	if err != nil {
		return nil, "", err
	}
	if !before.Mode().IsRegular() || before.Mode().Perm() != mode || before.Size() <= 0 || before.Size() > maximum {
		return nil, "", fmt.Errorf("formatter bundle file has invalid type, mode, or size: mode=%s size=%d expected=%#o maximum=%d", before.Mode(), before.Size(), mode, maximum)
	}
	if err := ownedByOperator(before); err != nil {
		return nil, "", err
	}
	if stat, ok := before.Sys().(*syscall.Stat_t); !ok || stat.Nlink != 1 {
		return nil, "", fmt.Errorf("formatter bundle file is not one-link")
	}
	if err := checkPrivateACL(path, before); err != nil {
		return nil, "", err
	}
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, "", err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(before, opened) {
		return nil, "", fmt.Errorf("formatter bundle file changed while opening: %v", err)
	}
	contents, err := io.ReadAll(io.LimitReader(file, maximum+1))
	if err != nil || int64(len(contents)) != before.Size() || int64(len(contents)) > maximum {
		return nil, "", fmt.Errorf("formatter bundle file changed while hashing: %v", err)
	}
	after, err := os.Lstat(path)
	if err != nil || !os.SameFile(before, after) || after.Mode() != before.Mode() || after.Size() != before.Size() {
		return nil, "", fmt.Errorf("formatter bundle file changed while reading: %v", err)
	}
	sum := sha256.Sum256(contents)
	return contents, hex.EncodeToString(sum[:]), nil
}

func checkVZCommand(ctx context.Context, runner execx.Runner, command execx.Command) (execx.Result, error) {
	command.Env = []string{"PATH=/usr/bin:/bin", "LANG=C", "LC_ALL=C"}
	result, err := runner.Run(ctx, command)
	if err != nil {
		return execx.Result{}, fmt.Errorf("formatter bundle signature check failed: %w", err)
	}
	if result.Truncated {
		return execx.Result{}, fmt.Errorf("formatter bundle signature check output overflow")
	}
	return result, nil
}

func admitVZBundle(ctx context.Context, path, stateRoot string, selected domain.ID, sourceCommit string, runner execx.Runner, pins formatterPins) (vzBundle, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return vzBundle{}, fmt.Errorf("formatter bundle path is not clean and absolute")
	}
	info, err := os.Lstat(path)
	if err != nil {
		return vzBundle{}, err
	}
	if err := privateDirectory(info); err != nil {
		return vzBundle{}, err
	}
	if err := checkPrivateACL(path, info); err != nil {
		return vzBundle{}, err
	}
	raw, _, err := bundleFile(filepath.Join(path, "manifest.json"), 0o600, 4096)
	if err != nil {
		return vzBundle{}, err
	}
	if err := rejectDuplicateJSON(raw); err != nil {
		return vzBundle{}, fmt.Errorf("formatter manifest has duplicate JSON: %w", err)
	}
	var manifest vzBundleDocument
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return vzBundle{}, err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return vzBundle{}, fmt.Errorf("formatter manifest has trailing content")
	}
	if manifest.Version != 1 || !lowerHex(sourceCommit, 40) || manifest.SourceCommit != sourceCommit ||
		manifest.ISOSHA256 != pins.iso || manifest.CheckerDebSHA256 != pins.deb ||
		manifest.ManagedStateRoot != stateRoot || manifest.ManagedDomain != string(selected) ||
		len(manifest.RunnerEntitlements) != 1 || !manifest.RunnerEntitlements["com.apple.security.virtualization"] {
		return vzBundle{}, fmt.Errorf("formatter bundle does not bind the admitted source and managed state")
	}
	limits := map[string]struct {
		mode os.FileMode
		max  int64
	}{
		"kernel-image":         {0o600, 128 << 20},
		"formatter-initrd":     {0o600, 256 << 20},
		"alpha-formatter":      {0o700, 64 << 20},
		"e2fsck.static":        {0o600, 16 << 20},
		"alpha-formatter-host": {0o700, 16 << 20},
		"binding.swift":        {0o600, 4096},
	}
	if len(manifest.Files) != len(limits) || manifest.Files["kernel-image"] != pins.kernel || manifest.Files["e2fsck.static"] != pins.checker {
		return vzBundle{}, fmt.Errorf("formatter bundle artifact pins differ")
	}
	for name, constraint := range limits {
		expected := manifest.Files[name]
		if !lowerHex(expected, 64) {
			return vzBundle{}, fmt.Errorf("formatter bundle artifact digest is missing")
		}
		contents, actual, err := bundleFile(filepath.Join(path, name), constraint.mode, constraint.max)
		if err != nil {
			return vzBundle{}, fmt.Errorf("formatter bundle artifact %q: %w", name, err)
		}
		if actual != expected {
			return vzBundle{}, fmt.Errorf("formatter bundle artifact %q digest differs", name)
		}
		if name == "binding.swift" && string(contents) != managedBindingSource(stateRoot, selected) {
			return vzBundle{}, fmt.Errorf("formatter signed binding source differs")
		}
	}
	vmRunner := filepath.Join(path, "alpha-formatter-host")
	if _, err := checkVZCommand(ctx, runner, execx.Command{Path: "/usr/bin/codesign", Args: []string{"--verify", "--strict", vmRunner}}); err != nil {
		return vzBundle{}, err
	}
	entitlements, err := checkVZCommand(ctx, runner, execx.Command{Path: "/usr/bin/codesign", Args: []string{"-d", "--entitlements", ":-", vmRunner}})
	if err != nil {
		return vzBundle{}, err
	}
	converted, err := checkVZCommand(ctx, runner, execx.Command{Path: "/usr/bin/plutil", Args: []string{"-convert", "json", "-o", "-", "-"}, Stdin: []byte(entitlements.Stdout)})
	if err != nil {
		return vzBundle{}, err
	}
	if err := rejectDuplicateJSON([]byte(converted.Stdout)); err != nil {
		return vzBundle{}, err
	}
	var actualEntitlements map[string]bool
	if err := json.Unmarshal([]byte(converted.Stdout), &actualEntitlements); err != nil || len(actualEntitlements) != 1 || !actualEntitlements["com.apple.security.virtualization"] {
		return vzBundle{}, fmt.Errorf("formatter runner has unexpected entitlements")
	}
	return vzBundle{runner: vmRunner, kernel: filepath.Join(path, "kernel-image"), initrd: filepath.Join(path, "formatter-initrd")}, nil
}
