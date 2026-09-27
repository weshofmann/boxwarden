package exportx

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/weshofmann/boxwarden/internal/execx"
)

var inspectorStaticSources = []string{
	"go.mod",
	"tools/alpha-inspector/main.swift",
	"tools/alpha-inspector/boot.swift",
	"tools/alpha-inspector/virtualization.entitlements",
	"tools/alpha-inspector/pack_initramfs.py",
	"tools/alpha-inspector/kernel_image.py",
	"tools/alpha-inspector/prepare_export_bundle.sh",
}

type inspectorSourceIdentity struct {
	commit string
	files  map[string]string
}

// inspectorSourceInputs binds artifact admission to the complete tracked
// executable input inventory in a clean checkout. Commit is provenance; the
// file map is the admission identity, so docs-only commits do not invalidate
// a qualified bundle.
func inspectorSourceInputs(ctx context.Context, root string) (inspectorSourceIdentity, error) {
	if !filepath.IsAbs(root) || filepath.Clean(root) != root {
		return inspectorSourceIdentity{}, fmt.Errorf("inspector source root is not clean and absolute")
	}
	runner := execx.OSRunner{MaxOutputBytes: 16 << 10, MaxStdinBytes: 0}
	run := func(args ...string) (string, error) {
		result, err := runner.Run(ctx, execx.Command{Path: "/usr/bin/git", Args: append([]string{"-C", root}, args...),
			Env: []string{"PATH=/usr/bin:/bin", "LANG=C", "LC_ALL=C"}})
		if err != nil || result.Truncated {
			return "", fmt.Errorf("inspector source Git check failed: %v", err)
		}
		return result.Stdout, nil
	}
	top, err := run("rev-parse", "--show-toplevel")
	canonicalRoot, rootErr := filepath.EvalSymlinks(root)
	canonicalTop, topErr := filepath.EvalSymlinks(strings.TrimSpace(top))
	if err != nil || rootErr != nil || topErr != nil || canonicalTop != canonicalRoot {
		return inspectorSourceIdentity{}, fmt.Errorf("inspector source root differs from Git top level: %v", err)
	}
	commit, err := run("rev-parse", "HEAD")
	commit = strings.TrimSpace(commit)
	if err != nil || !lowerHexBundle(commit, 40) {
		return inspectorSourceIdentity{}, fmt.Errorf("inspector source revision is invalid: %v", err)
	}
	status, err := run("status", "--porcelain", "--untracked-files=all")
	if err != nil || status != "" {
		return inspectorSourceIdentity{}, fmt.Errorf("inspector source checkout is not clean: %v", err)
	}
	args := append([]string{"ls-files", "-z", "--"}, inspectorStaticSources...)
	args = append(args, "tools/alpha-inspector/guest")
	listing, err := run(args...)
	if err != nil {
		return inspectorSourceIdentity{}, err
	}
	var names []string
	for _, name := range strings.Split(listing, "\x00") {
		if name != "" {
			names = append(names, name)
		}
	}
	if len(names) < len(inspectorStaticSources)+1 || len(names) > 64 {
		return inspectorSourceIdentity{}, fmt.Errorf("inspector source inventory has invalid count")
	}
	static := make(map[string]bool, len(inspectorStaticSources))
	for _, name := range inspectorStaticSources {
		static[name] = true
	}
	files := make(map[string]string, len(names))
	guestCount := 0
	for _, name := range names {
		if _, exists := files[name]; exists || !validInspectorSourceName(name, static) {
			return inspectorSourceIdentity{}, fmt.Errorf("inspector source inventory has duplicate or unexpected path")
		}
		if strings.HasPrefix(name, "tools/alpha-inspector/guest/") {
			guestCount++
		}
		digest, err := hashInspectorSource(filepath.Join(root, name))
		if err != nil {
			return inspectorSourceIdentity{}, fmt.Errorf("inspector source %q: %w", name, err)
		}
		files[name] = digest
	}
	if guestCount == 0 {
		return inspectorSourceIdentity{}, fmt.Errorf("inspector guest source inventory is empty")
	}
	for name := range static {
		if _, ok := files[name]; !ok {
			return inspectorSourceIdentity{}, fmt.Errorf("inspector tracked build input %q is missing", name)
		}
	}
	return inspectorSourceIdentity{commit: commit, files: files}, nil
}

func validInspectorSourceName(name string, static map[string]bool) bool {
	if static[name] {
		return true
	}
	if !strings.HasPrefix(name, "tools/alpha-inspector/guest/") || !strings.HasSuffix(name, ".go") {
		return false
	}
	base := strings.TrimPrefix(name, "tools/alpha-inspector/guest/")
	return base != "" && base != ".go" && !strings.Contains(base, "/") && !strings.Contains(base, "\\")
}

func hashInspectorSource(path string) (string, error) {
	before, err := os.Lstat(path)
	if err != nil || !before.Mode().IsRegular() || before.Size() <= 0 || before.Size() > 1<<20 {
		return "", fmt.Errorf("not a bounded regular source file: %v", err)
	}
	stat, ok := before.Sys().(*syscall.Stat_t)
	if !ok || stat.Nlink != 1 {
		return "", fmt.Errorf("source file is not one-link")
	}
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return "", err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(before, opened) || before.Mode() != opened.Mode() || before.Size() != opened.Size() {
		return "", fmt.Errorf("source changed while opening: %v", err)
	}
	hash := sha256.New()
	count, err := io.Copy(hash, io.LimitReader(file, 1<<20+1))
	if err != nil || count != before.Size() {
		return "", fmt.Errorf("source changed while hashing: %v", err)
	}
	after, err := os.Lstat(path)
	if err != nil || !os.SameFile(before, after) || after.Mode() != before.Mode() || after.Size() != before.Size() {
		return "", fmt.Errorf("source path changed during admission: %v", err)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func lowerHexBundle(value string, length int) bool {
	if len(value) != length {
		return false
	}
	for i := range value {
		if value[i] < '0' || value[i] > '9' && value[i] < 'a' || value[i] > 'f' {
			return false
		}
	}
	return true
}
