package workspacex

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"time"

	"github.com/weshofmann/boxwarden/internal/diskreserve"
	"github.com/weshofmann/boxwarden/internal/domain"
	"github.com/weshofmann/boxwarden/internal/exportx"
	"github.com/weshofmann/boxwarden/internal/lock"
)

const inspectorBuildDeadline = 5 * time.Minute
const inspectorBuildLogLimit = 16 << 10

type exportInspectorBundleBuilder func(context.Context, string, string, string, string) (string, error)
type exportInspectorBundleAdmitter func(context.Context, string, string, []byte) (exportx.InspectorBundle, error)

type PreparedInspectorBundle struct {
	Path     string
	identity os.FileInfo
}

// Remove cleans only the exact private directory returned by Build.
func (bundle PreparedInspectorBundle) Remove() error {
	if err := exactPreparedInspectorBundlePath(bundle.Path); err != nil {
		return err
	}
	info, err := os.Lstat(bundle.Path)
	if err != nil {
		return err
	}
	if bundle.identity == nil || privateDirectory(info) != nil || !os.SameFile(info, bundle.identity) {
		return fmt.Errorf("inspector bundle identity changed before cleanup")
	}
	root, err := os.OpenRoot("/private/tmp")
	if err != nil {
		return err
	}
	defer root.Close()
	return root.RemoveAll(filepath.Base(bundle.Path))
}

// BuildAdmittedExportInspectorBundle builds one private request-bearing bundle
// from a snapshot-ready journal and returns its admitted directory and inode
// receipt. The caller owns that private bundle and must remove it after capture. No VM runs
// and no export is published here.
func BuildAdmittedExportInspectorBundle(ctx context.Context, stateRoot string, domainID domain.ID, transactionID, sourceRoot, isoPath, goBinary string) (PreparedInspectorBundle, error) {
	return buildAdmittedExportInspectorBundle(ctx, stateRoot, domainID, transactionID, sourceRoot, isoPath, goBinary,
		runTrustedInspectorBuilder, exportx.AdmitInspectorBundle)
}

func buildAdmittedExportInspectorBundle(ctx context.Context, stateRoot string, domainID domain.ID, transactionID, sourceRoot, isoPath, goBinary string,
	build exportInspectorBundleBuilder, admit exportInspectorBundleAdmitter) (bundle PreparedInspectorBundle, err error) {
	if _, parseErr := domain.Parse(string(domainID)); parseErr != nil || !validUUID(transactionID) || build == nil || admit == nil {
		return bundle, fmt.Errorf("invalid inspector build transaction: %v", parseErr)
	}
	for _, input := range []string{sourceRoot, isoPath, goBinary} {
		if !filepath.IsAbs(input) || filepath.Clean(input) != input || strings.ContainsAny(input, "\r\n\x00") {
			return bundle, fmt.Errorf("inspector build inputs must be clean absolute paths")
		}
	}
	if filepath.Base(goBinary) != "go" {
		return bundle, fmt.Errorf("inspector build requires an explicit Go executable")
	}
	held, err := lock.Acquire(ctx, stateRoot, "export-"+string(domainID)+"-"+transactionID)
	if err != nil {
		return bundle, err
	}
	defer func() {
		err = errors.Join(err, held.Release())
		if err != nil && bundle.identity != nil {
			err = errors.Join(err, bundle.Remove())
			bundle = PreparedInspectorBundle{}
		}
	}()
	prepared, err := prepareExportInspectorRequestLocked(ctx, stateRoot, domainID, transactionID)
	if err != nil {
		return bundle, err
	}
	requestDir, err := os.MkdirTemp("/private/tmp", "boxwarden-alpha-export-request.")
	if err != nil {
		return bundle, err
	}
	defer func() { err = errors.Join(err, os.RemoveAll(requestDir)) }()
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
	buildContext, cancel := context.WithTimeout(ctx, inspectorBuildDeadline)
	defer cancel()
	err = diskreserve.Run(buildContext, []string{stateRoot, "/private/tmp"}, func(guarded context.Context) error {
		var buildErr error
		bundle.Path, buildErr = build(guarded, sourceRoot, isoPath, requestPath, goBinary)
		if buildErr != nil {
			return buildErr
		}
		if err := exactPreparedInspectorBundlePath(bundle.Path); err != nil {
			return err
		}
		info, err := os.Lstat(bundle.Path)
		if err != nil || privateDirectory(info) != nil {
			return fmt.Errorf("builder output is not a private directory: %v", err)
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

func exactPreparedInspectorBundlePath(path string) error {
	const prefix = "/private/tmp/boxwarden-alpha-inspector-export."
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
func runTrustedInspectorBuilder(ctx context.Context, sourceRoot, isoPath, requestPath, goBinary string) (string, error) {
	info, err := os.Lstat(goBinary)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return "", fmt.Errorf("explicit Go executable is not regular and executable: %v", err)
	}
	script := filepath.Join(sourceRoot, "tools", "alpha-inspector", "prepare_export_bundle.sh")
	command := exec.CommandContext(ctx, "/bin/bash", script, isoPath, requestPath)
	command.Env = []string{"PATH=" + filepath.Dir(goBinary) + ":/usr/bin:/bin", "LANG=C", "LC_ALL=C"}
	command.Stdin = nil
	command.WaitDelay = 5 * time.Second
	stdout, stderr := &boundedBuildLog{limit: inspectorBuildLogLimit}, &boundedBuildLog{limit: inspectorBuildLogLimit}
	command.Stdout, command.Stderr = stdout, stderr
	if err := command.Run(); err != nil {
		return "", fmt.Errorf("inspector builder failed: %w; stderr: %s", err, stderr.String())
	}
	if stdout.overflow || stderr.overflow {
		return "", fmt.Errorf("inspector builder output exceeded bound")
	}
	const marker = "prepared private export inspector artifacts: "
	output := stdout.String()
	if !strings.HasPrefix(output, marker) || !strings.HasSuffix(output, "\n") || strings.Count(output, "\n") != 1 {
		return "", fmt.Errorf("inspector builder did not return one exact artifact path")
	}
	path := strings.TrimSuffix(strings.TrimPrefix(output, marker), "\n")
	if err := exactPreparedInspectorBundlePath(path); err != nil {
		return "", err
	}
	return path, nil
}

type boundedBuildLog struct {
	bytes.Buffer
	limit    int
	overflow bool
}

func (log *boundedBuildLog) Write(input []byte) (int, error) {
	remaining := log.limit - log.Len()
	if remaining <= 0 {
		log.overflow = true
		return len(input), nil
	}
	if len(input) > remaining {
		log.overflow = true
		_, _ = log.Buffer.Write(input[:remaining])
		return len(input), nil
	}
	_, _ = log.Buffer.Write(input)
	return len(input), nil
}
