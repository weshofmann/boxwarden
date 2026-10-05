package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/weshofmann/boxwarden/internal/app"
	"github.com/weshofmann/boxwarden/internal/basebuild"
	"github.com/weshofmann/boxwarden/internal/config"
	"github.com/weshofmann/boxwarden/internal/execx"
	"github.com/weshofmann/boxwarden/internal/projectx"
	"github.com/weshofmann/boxwarden/internal/workspaceformat"
)

// prepareSetup is an explicit operator action. It invokes only the source-bound
// package helper, never host initialization, privilege installation, or a guest.
func prepareSetup(ctx context.Context, path string, input app.SetupPrepareInput, out io.Writer) (app.SetupInspection, bool, error) {
	if input.Prebuilt {
		resolved, _, err := preflightFirstRunResources(ctx, app.FirstRunInput{PackageRoot: input.PackageRoot, ISOPath: input.ISOPath})
		if err != nil {
			return app.SetupInspection{}, false, err
		}
		input = resolved
	}
	if input.PrebuiltResources != "" {
		return preparePrebuiltSetup(ctx, path, input, out)
	}
	fail := func(err error) (app.SetupInspection, bool, error) { return app.SetupInspection{}, false, err }
	executable, err := os.Executable()
	if err != nil {
		return fail(err)
	}
	if err := admitSetupPackage(ctx, input.PackageRoot, executable, buildRevision); err != nil {
		return fail(err)
	}
	for _, asset := range []string{input.ISOPath, input.CheckerPath, input.GoBinary, input.ZstdBinary, input.OpenSSLPath, input.XorrisoPath} {
		if err := admitSetupFile(asset); err != nil {
			return fail(fmt.Errorf("setup asset: %w", err))
		}
	}
	for _, tool := range []struct{ path, name string }{{input.GoBinary, "go"}, {input.ZstdBinary, "zstd"}, {input.OpenSSLPath, "openssl"}, {input.XorrisoPath, "xorriso"}} {
		info, err := os.Lstat(tool.path)
		if err != nil {
			return fail(err)
		}
		if filepath.Base(tool.path) != tool.name || info.Mode().Perm()&0111 == 0 {
			return fail(fmt.Errorf("setup requires the exact executable %s", tool.name))
		}
	}
	if err := checkProjectGoBinary(input.GoBinary); err != nil {
		return fail(err)
	}
	options := publicOptions(out)
	state := app.InspectSetup(ctx, path, options)
	if err := ctx.Err(); err != nil {
		return fail(err)
	}
	switch state.Status {
	case "domain_uninitialized", "project_setup_missing":
	default:
		return fail(fmt.Errorf("setup preparation is unavailable in state %s; %s", state.Status, state.Guidance))
	}
	// Existing saved setup is immutable here. Do not prepare expensive assets
	// only to discover that setup-update would be required.
	loaded, err := config.Load(path)
	if err != nil {
		return fail(err)
	}
	d, err := loaded.Domain("alpha")
	if err != nil {
		return fail(err)
	}
	if _, err := projectx.LoadSetup(d.StateRoot); !errors.Is(err, os.ErrNotExist) {
		return fail(errors.New("project setup already exists or is invalid; explicit setup-update is required"))
	}
	if state.Status == "domain_uninitialized" {
		// Deliberately do not expose any CA identity/error payload in progress.
		domainOptions := publicOptions(io.Discard)
		if err := app.Run(ctx, []string{"--config", path, "--domain", "alpha", "domain", "init"}, domainOptions); err != nil {
			return app.SetupInspection{}, true, errors.New("explicit alpha domain initialization failed; inspect domain state before retrying")
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	// The helper pins its own tool PATH and offline Go environment. Do not
	// inherit shell initialization or credential-bearing environment variables.
	home, err := os.UserHomeDir()
	if err != nil {
		return app.SetupInspection{}, true, err
	}
	command := execx.Command{Path: "/bin/bash", Args: []string{filepath.Join(input.PackageRoot, "prepare-projects.sh"), path, input.ISOPath, input.CheckerPath, input.GoBinary, input.ZstdBinary, input.OpenSSLPath, input.XorrisoPath}, Env: []string{"PATH=/usr/bin:/bin", "HOME=" + home, "LANG=C", "LC_ALL=C", "TMPDIR=" + os.TempDir()}}
	if _, err := (basebuild.OSOwnedScriptRunner{Output: out}).RunOwned(ctx, command); err != nil {
		return app.SetupInspection{}, true, fmt.Errorf("package setup helper failed; retained state may need inspection: %w", err)
	}

	result := app.InspectSetup(ctx, path, options)
	if err := ctx.Err(); err != nil {
		return result, true, err
	}
	if result.Status != "ready" {
		return result, true, fmt.Errorf("package setup completed but prerequisites remain %s; %s", result.Status, result.Guidance)
	}
	return result, true, nil
}

// The trusted operator may explicitly run package code. This admission does
// not treat a path supplied by guest-originated content as such authorization.
func admitSetupPackage(ctx context.Context, root, executable, revision string) error {
	if !canonicalProjectSetupPath(root) {
		return errors.New("package root must be a clean absolute path")
	}
	canonical, err := filepath.EvalSymlinks(root)
	if err != nil || canonical != root {
		return errors.New("package root is unavailable or has symlink ancestry")
	}
	info, err := os.Lstat(root)
	if err != nil || !info.IsDir() {
		return errors.New("package root must be a directory")
	}
	if err := operatorSetupEntry(info); err != nil {
		return err
	}
	source := filepath.Join(root, "support", "source")
	resolvedSource, err := filepath.EvalSymlinks(source)
	if err != nil || resolvedSource != source {
		return errors.New("package source has unavailable or symlink ancestors")
	}
	supportInfo, err := os.Lstat(filepath.Join(root, "support"))
	if err != nil || !supportInfo.IsDir() {
		return errors.New("package support must be a direct directory")
	}
	if err := operatorSetupEntry(supportInfo); err != nil {
		return err
	}
	// Check all source entries before invoking Git: repository configuration is
	// executable-adjacent input and must belong to the trusted operator too.
	count := 0
	if err := filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		count++
		if count > 100000 {
			return errors.New("package source exceeds entry limit")
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || (!info.IsDir() && !info.Mode().IsRegular()) {
			return errors.New("package source contains a symlink or special file")
		}
		return operatorSetupEntry(info)
	}); err != nil {
		return fmt.Errorf("package source admission: %w", err)
	}
	commit, err := workspaceformat.InspectSourceCommit(ctx, source)
	if err != nil {
		return err
	}
	if revision == "unknown" || revision == "" || commit != revision {
		return errors.New("package source revision differs from the invoking CLI build revision")
	}
	cli := filepath.Join(root, "bin", "boxwarden")
	if err := admitOperatorSetupFile(cli); err != nil {
		return err
	}
	installedDigest, err := setupFileDigest(executable)
	if err != nil {
		return err
	}
	packageDigest, err := setupFileDigest(cli)
	if err != nil {
		return err
	}
	if installedDigest != packageDigest {
		return errors.New("package CLI differs from the invoking executable")
	}
	script := filepath.Join(root, "prepare-projects.sh")
	if err := admitOperatorSetupFile(script); err != nil {
		return err
	}
	sourceScript := filepath.Join(source, "tools", "private-beta", "prepare-projects.sh")
	if err := admitSetupFile(sourceScript); err != nil {
		return err
	}
	scriptBytes, err := readSetupScript(script)
	if err != nil {
		return err
	}
	sourceBytes, err := readSetupScript(sourceScript)
	if err != nil {
		return err
	}
	if !bytes.Equal(scriptBytes, sourceBytes) {
		return errors.New("package preparation script differs from its clean source")
	}
	return nil
}

func operatorSetupEntry(info os.FileInfo) error {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(stat.Uid) != os.Getuid() || info.Mode().Perm()&0022 != 0 {
		return errors.New("package entry must be operator-owned and not writable by group or others")
	}
	return nil
}
func admitSetupFile(path string) error {
	if !canonicalProjectSetupPath(path) {
		return errors.New("setup input requires a clean absolute path")
	}
	canonical, err := filepath.EvalSymlinks(path)
	if err != nil || canonical != path {
		return errors.New("setup input is unavailable or has symlink ancestry")
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("setup input must be a regular direct file")
	}
	// Installed tool binaries may be root-owned; package entries require the
	// stronger exact operator ownership check at their call sites.
	if info.Mode().Perm()&0022 != 0 {
		return errors.New("setup input must not be writable by group or others")
	}
	return nil
}
func setupFileDigest(path string) ([32]byte, error) {
	var digest [32]byte
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return digest, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return digest, err
	}
	if !info.Mode().IsRegular() || info.Size() > 128<<20 {
		return digest, errors.New("package CLI exceeds size limit or is not a regular file")
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, f); err != nil {
		return digest, err
	}
	copy(digest[:], hash.Sum(nil))
	return digest, nil
}
func readSetupScript(path string) ([]byte, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, 128<<10+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > 128<<10 {
		return nil, errors.New("package preparation script exceeds limit")
	}
	return raw, nil
}

func admitOperatorSetupFile(path string) error {
	if err := admitSetupFile(path); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	return operatorSetupEntry(info)
}
