package workspacex

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"time"

	"github.com/weshofmann/boxwarden/internal/diskreserve"
	"github.com/weshofmann/boxwarden/internal/domain"
	"github.com/weshofmann/boxwarden/internal/exportx"
	"github.com/weshofmann/boxwarden/internal/lock"
	"github.com/weshofmann/boxwarden/internal/workspaceformat"
)

const inspectorBuildDeadline = 5 * time.Minute
const inspectorBuildLogLimit = 16 << 10

type exportInspectorBundleBuilder func(context.Context, string, string, string, string, string) (string, error)
type exportInspectorBundleAdmitter func(context.Context, string, string, []byte) (exportx.InspectorBundle, error)

type PreparedInspectorBundle struct {
	Path           string
	identity       os.FileInfo
	parentPath     string
	parentIdentity os.FileInfo
}

// Remove cleans only the exact private directory returned by Build.
func (bundle PreparedInspectorBundle) Remove() error {
	if err := exactPreparedInspectorBundlePath(bundle.Path, bundle.parentPath); err != nil {
		return err
	}
	root, err := openStateRoot(bundle.parentPath)
	if err != nil {
		return err
	}
	defer root.Close()
	parent, err := root.Stat(".")
	if err != nil || bundle.parentIdentity == nil || !os.SameFile(parent, bundle.parentIdentity) {
		return fmt.Errorf("inspector staging parent identity changed before cleanup: %v", err)
	}
	info, err := root.Lstat(filepath.Base(bundle.Path))
	if err != nil {
		return err
	}
	if bundle.identity == nil || privateDirectory(info) != nil || !os.SameFile(info, bundle.identity) {
		return fmt.Errorf("inspector bundle identity changed before cleanup")
	}
	if err := checkPrivateACL(bundle.Path, info); err != nil {
		return err
	}
	return root.RemoveAll(filepath.Base(bundle.Path))
}

// BuildAdmittedExportInspectorBundle builds one private request-bearing bundle
// from a snapshot-ready journal (or inspected retry) and returns its directory and inode
// receipt. The caller owns that private bundle and must remove it after capture. No VM runs
// and no export is published here.
func BuildAdmittedExportInspectorBundle(ctx context.Context, stateRoot string, domainID domain.ID, transactionID, sourceRoot, isoPath, goBinary string) (PreparedInspectorBundle, error) {
	return buildAdmittedExportInspectorBundle(ctx, stateRoot, domainID, transactionID, sourceRoot, isoPath, goBinary,
		runTrustedInspectorBuilder, exportx.AdmitInspectorBundle)
}

func buildAdmittedExportInspectorBundle(ctx context.Context, stateRoot string, domainID domain.ID, transactionID, sourceRoot, isoPath, goBinary string,
	build exportInspectorBundleBuilder, admit exportInspectorBundleAdmitter) (bundle PreparedInspectorBundle, err error) {
	return buildAdmittedExportInspectorBundleMode(ctx, stateRoot, domainID, transactionID, sourceRoot, isoPath, goBinary, build, admit, false)
}

func BuildAdmittedPrebuiltExportInspectorBundle(ctx context.Context, stateRoot string, domainID domain.ID, transactionID, sourceRoot, isoPath, resourcesRoot string) (PreparedInspectorBundle, error) {
	if _, err := workspaceformat.CheckPrebuiltSupport(ctx, sourceRoot, resourcesRoot); err != nil {
		return PreparedInspectorBundle{}, err
	}
	builder := func(ctx context.Context, source, iso, request, _, output string) (string, error) {
		return runTrustedPrebuiltInspectorBuilder(ctx, source, iso, request, resourcesRoot, output)
	}
	return buildAdmittedExportInspectorBundleMode(ctx, stateRoot, domainID, transactionID, sourceRoot, isoPath, "", builder, exportx.AdmitInspectorBundle, true)
}

func buildAdmittedExportInspectorBundleMode(ctx context.Context, stateRoot string, domainID domain.ID, transactionID, sourceRoot, isoPath, goBinary string, build exportInspectorBundleBuilder, admit exportInspectorBundleAdmitter, prebuilt bool) (bundle PreparedInspectorBundle, err error) {
	if _, parseErr := domain.Parse(string(domainID)); parseErr != nil || !validUUID(transactionID) || build == nil || admit == nil {
		return bundle, fmt.Errorf("invalid inspector build transaction: %v", parseErr)
	}
	inputs := []string{sourceRoot, isoPath}
	if !prebuilt {
		inputs = append(inputs, goBinary)
	}
	for _, input := range inputs {
		if !filepath.IsAbs(input) || filepath.Clean(input) != input || strings.ContainsAny(input, "\r\n\x00") {
			return bundle, fmt.Errorf("inspector build inputs must be clean absolute paths")
		}
	}
	if !prebuilt && filepath.Base(goBinary) != "go" {
		return bundle, fmt.Errorf("inspector build requires an explicit Go executable")
	}
	held, err := lock.Acquire(ctx, stateRoot, "export-"+string(domainID)+"-"+transactionID)
	if err != nil {
		return bundle, err
	}
	defer func() {
		err = errors.Join(err, held.Release())
		if err != nil && bundle.identity != nil && !errors.Is(err, errExportBuilderLifetimeUnproven) {
			err = errors.Join(err, bundle.Remove())
			bundle = PreparedInspectorBundle{}
		}
	}()
	prepared, err := prepareExportInspectorRequestLocked(ctx, stateRoot, domainID, transactionID)
	if err != nil {
		return bundle, err
	}
	state, err := openStateRoot(stateRoot)
	if err != nil {
		return bundle, err
	}
	defer state.Close()
	staging, err := openChild(state, "export-builds", true)
	if err != nil {
		return bundle, err
	}
	defer staging.Close()
	bundle.parentPath = staging.Name()
	bundle.parentIdentity, err = staging.Stat(".")
	if err != nil {
		return bundle, err
	}
	requestDir, err := os.MkdirTemp(staging.Name(), "boxwarden-alpha-export-request.")
	if err != nil {
		return bundle, err
	}
	requestIdentity, err := staging.Lstat(filepath.Base(requestDir))
	if err != nil {
		return bundle, err
	}
	defer func() {
		if errors.Is(err, errExportBuilderLifetimeUnproven) {
			err = fmt.Errorf("%w; retained inspector request %s and output %s", err, requestDir, bundle.Path)
			bundle = PreparedInspectorBundle{}
			return
		}
		info, statErr := staging.Lstat(filepath.Base(requestDir))
		if statErr != nil || privateDirectory(info) != nil || !os.SameFile(info, requestIdentity) {
			err = errors.Join(err, fmt.Errorf("inspector request directory identity changed before cleanup: %v", statErr))
			return
		}
		if aclErr := checkPrivateACL(requestDir, info); aclErr != nil {
			err = errors.Join(err, aclErr)
			return
		}
		err = errors.Join(err, staging.RemoveAll(filepath.Base(requestDir)))
	}()
	requestPath := filepath.Join(requestDir, "request.json")
	requestFile, err := os.OpenFile(requestPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return bundle, err
	}
	if _, err = requestFile.Write(prepared.Request); err == nil {
		err = requestFile.Sync()
	}
	err = errors.Join(err, requestFile.Close())
	if err != nil {
		return bundle, err
	}
	var suffix [3]byte
	if _, err = rand.Read(suffix[:]); err != nil {
		return bundle, err
	}
	bundle.Path = filepath.Join(staging.Name(), "boxwarden-alpha-inspector-export."+hex.EncodeToString(suffix[:]))
	if err = staging.Mkdir(filepath.Base(bundle.Path), 0o700); err != nil {
		bundle.Path = ""
		return bundle, err
	}
	bundle.identity, err = staging.Lstat(filepath.Base(bundle.Path))
	if err != nil {
		return bundle, err
	}
	if err = checkPrivateACL(bundle.Path, bundle.identity); err != nil {
		return bundle, err
	}
	buildContext, cancel := context.WithTimeout(ctx, inspectorBuildDeadline)
	defer cancel()
	err = diskreserve.Run(buildContext, []string{stateRoot, staging.Name()}, func(guarded context.Context) error {
		builtPath, buildErr := build(guarded, sourceRoot, isoPath, requestPath, goBinary, bundle.Path)
		if buildErr != nil {
			return buildErr
		}
		if builtPath != bundle.Path {
			return fmt.Errorf("builder returned a different inspector bundle path")
		}
		if err := exactPreparedInspectorBundlePath(bundle.Path, staging.Name()); err != nil {
			return err
		}
		info, err := os.Lstat(bundle.Path)
		if err != nil || privateDirectory(info) != nil || !os.SameFile(info, bundle.identity) {
			return fmt.Errorf("builder output is not a private directory: %v", err)
		}
		if err := checkPrivateACL(bundle.Path, info); err != nil {
			return err
		}
		parent, err := os.Lstat(staging.Name())
		if err != nil || !os.SameFile(parent, bundle.parentIdentity) {
			return fmt.Errorf("inspector staging parent changed while building: %v", err)
		}
		bundle.identity = info
		if _, err := admit(guarded, bundle.Path, sourceRoot, prepared.Request); err != nil {
			return fmt.Errorf("admit newly built inspector bundle: %w", err)
		}
		if err := admitExactExportSnapshot(guarded, stateRoot, prepared.Journal); err != nil {
			return fmt.Errorf("snapshot changed while building inspector bundle: %w", err)
		}
		current, err := loadExportJournal(stateRoot, domainID, transactionID)
		if err != nil || !reflect.DeepEqual(current, prepared.Journal) {
			return fmt.Errorf("journal changed while building inspector bundle: %v", err)
		}
		return nil
	})
	if err != nil {
		return bundle, err
	}
	return bundle, nil
}

func exactPreparedInspectorBundlePath(path, parent string) error {
	if !filepath.IsAbs(parent) || filepath.Clean(parent) != parent || strings.ContainsAny(parent, "\r\n\x00") {
		return fmt.Errorf("invalid inspector staging parent")
	}
	prefix := filepath.Join(parent, "boxwarden-alpha-inspector-export.")
	if !strings.HasPrefix(path, prefix) || len(path) != len(prefix)+6 || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return fmt.Errorf("builder returned unexpected inspector bundle path")
	}
	for _, c := range path[len(prefix):] {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9') {
			return fmt.Errorf("builder returned malformed inspector bundle name")
		}
	}
	return nil
}

// The executable is a tracked, clean-source build script with positional argv
// elements. This explicit Bash invocation cannot be replaced by guest input;
// both stdout and stderr are bounded and never include request bytes.
func runTrustedPrebuiltInspectorBuilder(ctx context.Context, sourceRoot, isoPath, requestPath, resourcesRoot, outputDir string) (string, error) {
	return runInspectorBuilder(ctx, sourceRoot, []string{isoPath, requestPath, outputDir, resourcesRoot}, []string{"PATH=/usr/bin:/bin", "LANG=C", "LC_ALL=C", "TMPDIR=" + outputDir}, outputDir)
}
func runTrustedInspectorBuilder(ctx context.Context, sourceRoot, isoPath, requestPath, goBinary, outputDir string) (string, error) {
	info, err := os.Lstat(goBinary)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return "", fmt.Errorf("explicit Go executable is not regular and executable: %v", err)
	}
	return runInspectorBuilder(ctx, sourceRoot, []string{isoPath, requestPath, outputDir}, []string{"PATH=" + filepath.Dir(goBinary) + ":/usr/bin:/bin", "LANG=C", "LC_ALL=C", "TMPDIR=" + outputDir}, outputDir)
}
func runInspectorBuilder(ctx context.Context, sourceRoot string, args, env []string, outputDir string) (string, error) {
	script := filepath.Join(sourceRoot, "tools", "alpha-inspector", "prepare_export_bundle.sh")
	stdout, stderr, err := runExportBuilderProcess(ctx, script, args, env)
	if err != nil {
		return "", fmt.Errorf("inspector builder failed: %w; stderr: %s", err, stderr)
	}
	const marker = "prepared private export inspector artifacts: "
	output := stdout
	if !strings.HasPrefix(output, marker) || !strings.HasSuffix(output, "\n") || strings.Count(output, "\n") != 1 {
		return "", fmt.Errorf("inspector builder did not return one exact artifact path")
	}
	path := strings.TrimSuffix(strings.TrimPrefix(output, marker), "\n")
	if err := exactPreparedInspectorBundlePath(path, filepath.Dir(outputDir)); err != nil {
		return "", err
	}
	if path != outputDir {
		return "", fmt.Errorf("builder returned a different inspector bundle path")
	}
	return path, nil
}

type boundedBuildLog struct {
	buffer   bytes.Buffer
	limit    int
	overflow bool
}

func (log *boundedBuildLog) Write(input []byte) (int, error) {
	remaining := log.limit - log.buffer.Len()
	if remaining <= 0 {
		log.overflow = true
		return len(input), nil
	}
	if len(input) > remaining {
		log.overflow = true
		_, _ = log.buffer.Write(input[:remaining])
		return len(input), nil
	}
	_, _ = log.buffer.Write(input)
	return len(input), nil
}

func (log *boundedBuildLog) String() string { return log.buffer.String() }
