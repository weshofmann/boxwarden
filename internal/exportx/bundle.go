package exportx

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"syscall"

	"github.com/weshofmann/boxwarden/internal/execx"
	"github.com/weshofmann/boxwarden/internal/privateacl"
)

const (
	inspectorISOSHA          = "c2610520bf582976839a1724c669e1cfed0547427be5a0ad12d457b92b46ffbe"
	inspectorSourceKernelSHA = "000d59171b8e49f31f55c0d52123571ca8963718220fa8bacee1d65fdcbad617"
	inspectorImageKernelSHA  = "a1586ff3cb7ced7c40dcb0aba5bf320ebb94a46d1a6505eb03157a8f9525632d"
)

type InspectorBundle struct {
	Helper string
	Kernel string
	Initrd string
}

type inspectorBundleManifest struct {
	Version            int               `json:"version"`
	SourceCommit       string            `json:"source_commit"`
	SourceInputs       map[string]string `json:"source_inputs"`
	ISOSHA256          string            `json:"iso_sha256"`
	Qualification      string            `json:"qualification"`
	Files              map[string]string `json:"files"`
	RunnerEntitlements map[string]bool   `json:"runner_entitlements"`
}

type inspectorBundlePins struct{ iso, sourceKernel, imageKernel string }

func productionInspectorBundlePins() inspectorBundlePins {
	return inspectorBundlePins{inspectorISOSHA, inspectorSourceKernelSHA, inspectorImageKernelSHA}
}

var bundleACLInspector privateacl.Inspector = privateacl.OSInspector{}

// AdmitInspectorBundle binds the private prepared bundle to current clean
// inspector source inputs and the exact journal-derived request bytes. The
// returned paths are executable launch inputs, not authorization to publish a
// stream: the caller must recheck the snapshot after helper reap and enforce
// export-mode evidence, journal selection, and phase transitions.
func AdmitInspectorBundle(ctx context.Context, bundlePath, sourceRoot string, request []byte) (InspectorBundle, error) {
	source, err := inspectorSourceInputs(ctx, sourceRoot)
	if err != nil {
		return InspectorBundle{}, err
	}
	return admitInspectorBundle(ctx, bundlePath, request, source, execx.OSRunner{MaxOutputBytes: 8 << 10, MaxStdinBytes: 8 << 10}, productionInspectorBundlePins())
}

func admitInspectorBundle(ctx context.Context, bundlePath string, request []byte, source inspectorSourceIdentity, runner execx.Runner, pins inspectorBundlePins) (InspectorBundle, error) {
	if !filepath.IsAbs(bundlePath) || filepath.Clean(bundlePath) != bundlePath || len(request) == 0 || len(request) > 128<<10 || runner == nil {
		return InspectorBundle{}, fmt.Errorf("inspector bundle admission lacks exact path, request, or runner")
	}
	parent, root, info, err := openPrivateParent(bundlePath)
	if err != nil {
		return InspectorBundle{}, err
	}
	defer parent.Close()
	defer root.Close()
	if err := privateacl.Check(bundlePath, info, bundleACLInspector); err != nil {
		return InspectorBundle{}, err
	}
	if err := exactBundleDirectory(root, []string{"alpha-inspector", "alpha-probe", "casper", "inspector-initrd", "kernel-image", "manifest.json", "request.json"}); err != nil {
		return InspectorBundle{}, err
	}
	casper, err := root.OpenRoot("casper")
	if err != nil {
		return InspectorBundle{}, err
	}
	defer casper.Close()
	casperInfo, err := root.Lstat("casper")
	if err != nil || privateParentInfo(casperInfo) != nil {
		return InspectorBundle{}, fmt.Errorf("inspector casper source directory is not private: %v", err)
	}
	if err := privateacl.Check(filepath.Join(bundlePath, "casper"), casperInfo, bundleACLInspector); err != nil {
		return InspectorBundle{}, err
	}
	if err := exactBundleDirectory(casper, []string{"initrd", "vmlinuz"}); err != nil {
		return InspectorBundle{}, err
	}
	manifestBytes, _, err := inspectBundleFile(filepath.Join(bundlePath, "manifest.json"), 0o600, 64<<10, "", true)
	if err != nil {
		return InspectorBundle{}, err
	}
	if err := rejectDuplicateBundleJSON(manifestBytes); err != nil {
		return InspectorBundle{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(manifestBytes))
	decoder.DisallowUnknownFields()
	var manifest inspectorBundleManifest
	if err := decoder.Decode(&manifest); err != nil {
		return InspectorBundle{}, err
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return InspectorBundle{}, fmt.Errorf("trailing inspector bundle manifest data")
	}
	if manifest.Version != 1 || manifest.Qualification != "production" ||
		!lowerHexBundle(manifest.SourceCommit, 40) || manifest.ISOSHA256 != pins.iso ||
		len(manifest.RunnerEntitlements) != 1 || !manifest.RunnerEntitlements["com.apple.security.virtualization"] ||
		!reflect.DeepEqual(manifest.SourceInputs, source.files) {
		return InspectorBundle{}, fmt.Errorf("inspector bundle does not bind clean executable inputs")
	}
	limits := map[string]struct {
		mode os.FileMode
		max  int64
	}{
		"casper/vmlinuz":   {0o400, 128 << 20},
		"casper/initrd":    {0o400, 256 << 20},
		"kernel-image":     {0o600, 128 << 20},
		"alpha-probe":      {0o700, 16 << 20},
		"inspector-initrd": {0o600, 256 << 20},
		"alpha-inspector":  {0o700, 16 << 20},
		"request.json":     {0o600, 128 << 10},
	}
	if len(manifest.Files) != len(limits) || manifest.Files["casper/vmlinuz"] != pins.sourceKernel || manifest.Files["kernel-image"] != pins.imageKernel {
		return InspectorBundle{}, fmt.Errorf("inspector bundle artifact inventory or fixed kernel pins differ")
	}
	for name, limit := range limits {
		if !lowerHexBundle(manifest.Files[name], 64) {
			return InspectorBundle{}, fmt.Errorf("inspector bundle artifact %q lacks exact digest", name)
		}
		content, _, err := inspectBundleFile(filepath.Join(bundlePath, name), limit.mode, limit.max, manifest.Files[name], name == "request.json")
		if err != nil {
			return InspectorBundle{}, fmt.Errorf("inspector bundle artifact %q: %w", name, err)
		}
		if name == "request.json" && !bytes.Equal(content, request) {
			return InspectorBundle{}, fmt.Errorf("inspector bundle request differs from journal-derived bytes")
		}
	}
	if err := inspectPackedInspectorInitrd(bundlePath, manifest.Files); err != nil {
		return InspectorBundle{}, err
	}
	if err := inspectInspectorSignature(ctx, runner, filepath.Join(bundlePath, "alpha-inspector")); err != nil {
		return InspectorBundle{}, err
	}
	if err := samePrivateParent(bundlePath, info, parent); err != nil {
		return InspectorBundle{}, err
	}
	return InspectorBundle{Helper: filepath.Join(bundlePath, "alpha-inspector"), Kernel: filepath.Join(bundlePath, "kernel-image"), Initrd: filepath.Join(bundlePath, "inspector-initrd")}, nil
}

func exactBundleDirectory(root *os.Root, wanted []string) error {
	directory, err := root.Open(".")
	if err != nil {
		return err
	}
	defer directory.Close()
	entries, err := directory.ReadDir(-1)
	if err != nil || len(entries) != len(wanted) {
		return fmt.Errorf("inspector bundle directory inventory differs: %v", err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for i, entry := range entries {
		if entry.Name() != wanted[i] {
			return fmt.Errorf("inspector bundle directory has unexpected entry")
		}
	}
	return nil
}

func inspectBundleFile(path string, mode os.FileMode, maximum int64, expected string, keep bool) ([]byte, int64, error) {
	before, err := os.Lstat(path)
	if err != nil || !before.Mode().IsRegular() || before.Mode().Perm() != mode || before.Size() <= 0 || before.Size() > maximum {
		return nil, 0, fmt.Errorf("private artifact type, mode, or size changed: %v", err)
	}
	stat, ok := before.Sys().(*syscall.Stat_t)
	if !ok || stat.Nlink != 1 || stat.Uid != uint32(os.Geteuid()) {
		return nil, 0, fmt.Errorf("private artifact ownership or link count changed")
	}
	if err := privateacl.Check(path, before, bundleACLInspector); err != nil {
		return nil, 0, err
	}
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, 0, err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(before, opened) {
		return nil, 0, fmt.Errorf("private artifact changed while opening: %v", err)
	}
	hash := sha256.New()
	var output bytes.Buffer
	writer := io.Writer(hash)
	if keep {
		writer = io.MultiWriter(hash, &output)
	}
	count, err := io.Copy(writer, io.LimitReader(file, maximum+1))
	if err != nil || count != before.Size() {
		return nil, 0, fmt.Errorf("private artifact changed while hashing: %v", err)
	}
	if expected != "" && hex.EncodeToString(hash.Sum(nil)) != expected {
		return nil, 0, fmt.Errorf("private artifact digest differs")
	}
	after, err := os.Lstat(path)
	if err != nil || !os.SameFile(before, after) || after.Mode() != before.Mode() || after.Size() != before.Size() {
		return nil, 0, fmt.Errorf("private artifact path changed: %v", err)
	}
	return output.Bytes(), count, nil
}
