package main

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
	"time"

	"github.com/weshofmann/boxwarden/internal/app"
	"github.com/weshofmann/boxwarden/internal/basebuild"
	"github.com/weshofmann/boxwarden/internal/config"
	"github.com/weshofmann/boxwarden/internal/execx"
	"github.com/weshofmann/boxwarden/internal/recipe"
	"github.com/weshofmann/boxwarden/internal/workspaceformat"
)

// Discovery is deliberately a finite search of conventional installed locations.
// It canonicalizes existing symlinks but never invokes a package manager.
func discoverSetupTool(name string, candidates []string) (string, string, error) {
	for _, candidate := range candidates {
		path, err := filepath.EvalSymlinks(candidate)
		if err != nil || filepath.Base(path) != name || admitSetupFile(path) != nil {
			continue
		}
		info, err := os.Lstat(path)
		if err != nil || info.Mode().Perm()&0111 == 0 {
			continue
		}
		digest, err := setupFileDigest(path)
		if err != nil {
			continue
		}
		return path, hex.EncodeToString(digest[:]), nil
	}
	return "", "", fmt.Errorf("compatible installed %s was not found; advanced setup accepts an exact existing executable; no tool was installed", name)
}

func preflightFirstRunResources(ctx context.Context, input app.FirstRunInput) (app.SetupPrepareInput, string, error) {
	return preflightFirstRunResourcesWithRunner(ctx, input, execx.OSRunner{MaxOutputBytes: 4096})
}

func checkPreparationHostSupport(ctx context.Context, runner execx.Runner) error {
	// Git, Python and codesign are the retained stock support tools. Do not let
	// macOS launch an installation prompt as part of an automatic setup action.
	probeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if _, err := runner.Run(probeCtx, execx.Command{Path: "/usr/bin/xcode-select", Args: []string{"-p"}, Env: []string{"PATH=/usr/bin:/bin", "LANG=C", "LC_ALL=C"}}); err != nil {
		return errors.New("Apple Command Line Tools are unavailable; complete the attended host prerequisites in the bundled guide before setup")
	}
	for _, path := range []string{"/usr/bin/python3", "/bin/bash", "/usr/bin/codesign", "/usr/bin/git"} {
		if _, err := os.Stat(path); err != nil {
			return fmt.Errorf("required host support tool %s is unavailable", filepath.Base(path))
		}
	}
	return nil
}

func preflightFirstRunResourcesWithRunner(ctx context.Context, input app.FirstRunInput, runner execx.Runner) (app.SetupPrepareInput, string, error) {
	prepared := app.SetupPrepareInput{PackageRoot: input.PackageRoot, ISOPath: input.ISOPath, PrebuiltResources: filepath.Join(input.PackageRoot, "support", "resources")}
	if err := checkPreparationHostSupport(ctx, runner); err != nil {
		return prepared, "", err
	}
	executable, err := os.Executable()
	if err != nil {
		return prepared, "", err
	}
	if err := admitSetupPackage(ctx, input.PackageRoot, executable, buildRevision); err != nil {
		return prepared, "", err
	}
	if err := recipe.VerifyISO(input.ISOPath); err != nil {
		return prepared, "", fmt.Errorf("choose Ubuntu 24.04.4 Desktop ARM64 with the bundled SHA-256 pin: %w", err)
	}
	isoInfo, err := os.Stat(input.ISOPath)
	if err != nil || isoInfo.Size() <= 0 || isoInfo.Size() > 4<<30 {
		return prepared, "", errors.New("verified installer size is unavailable or outside the admitted bound")
	}
	// Cold preparation retains a private staged input and a remastered ISO on
	// the state filesystem. Do not promise a first project from bootstrap-only
	// headroom; the live reserve guard still independently checks every write.
	prepared.BootstrapBytes = 2*uint64(isoInfo.Size()) + (256 << 20) + (1 << 30)
	source := filepath.Join(input.PackageRoot, "support", "source")
	resourcesDigest, err := workspaceformat.CheckPrebuiltSupport(ctx, source, prepared.PrebuiltResources)
	if err != nil {
		return prepared, "", fmt.Errorf("packaged support resources: %w", err)
	}
	var opensslDigest, xorrisoDigest string
	prepared.OpenSSLPath, opensslDigest, err = discoverSetupTool("openssl", []string{"/opt/homebrew/opt/openssl@3/bin/openssl", "/usr/local/opt/openssl@3/bin/openssl", "/opt/homebrew/bin/openssl", "/usr/local/bin/openssl", "/usr/bin/openssl"})
	if err != nil {
		return prepared, "", err
	}
	prepared.XorrisoPath, xorrisoDigest, err = discoverSetupTool("xorriso", []string{"/opt/homebrew/bin/xorriso", "/usr/local/bin/xorriso"})
	if err != nil {
		return prepared, "", err
	}
	seed := basebuild.HostSeedBuilder{Runner: runner, OpenSSLPath: prepared.OpenSSLPath, OpenSSLSHA256: opensslDigest, XorrisoPath: prepared.XorrisoPath, XorrisoSHA256: xorrisoDigest}
	if err := seed.CheckTools(); err != nil {
		return prepared, "", fmt.Errorf("installed preparation tools are incompatible: %w", err)
	}
	raw, err := json.Marshal(struct {
		Input                       app.SetupPrepareInput
		Resources, OpenSSL, Xorriso string
	}{prepared, resourcesDigest, opensslDigest, xorrisoDigest})
	if err != nil {
		return prepared, "", err
	}
	digest := sha256.Sum256(raw)
	return prepared, hex.EncodeToString(digest[:]), ctx.Err()
}

// Prepare a fresh per-configuration binding of build-time resources. Domain
// init and project setup still go through public commands and their admission.
func preparePrebuiltSetup(ctx context.Context, path string, input app.SetupPrepareInput, out io.Writer) (app.SetupInspection, bool, error) {
	if err := checkPreparationHostSupport(ctx, execx.OSRunner{MaxOutputBytes: 4096}); err != nil {
		return app.SetupInspection{}, false, err
	}
	fail := func(err error) (app.SetupInspection, bool, error) { return app.SetupInspection{}, false, err }
	executable, err := os.Executable()
	if err != nil {
		return fail(err)
	}
	if err := admitSetupPackage(ctx, input.PackageRoot, executable, buildRevision); err != nil {
		return fail(err)
	}
	source := filepath.Join(input.PackageRoot, "support", "source")
	if input.PrebuiltResources != filepath.Join(input.PackageRoot, "support", "resources") {
		return fail(errors.New("prebuilt setup must use this package's support resources"))
	}
	if _, err := workspaceformat.CheckPrebuiltSupport(ctx, source, input.PrebuiltResources); err != nil {
		return fail(err)
	}
	if err := recipe.VerifyISO(input.ISOPath); err != nil {
		return fail(err)
	}
	script := filepath.Join(source, "tools", "private-beta", "prepare_support.sh")
	if err := admitOperatorSetupFile(script); err != nil {
		return fail(err)
	}
	openDigest, err := setupFileDigest(input.OpenSSLPath)
	if err != nil {
		return fail(err)
	}
	xorDigest, err := setupFileDigest(input.XorrisoPath)
	if err != nil {
		return fail(err)
	}
	setupOptions := publicOptions(out)
	state := app.InspectSetup(ctx, path, setupOptions)
	if state.Status != "domain_uninitialized" && state.Status != "project_setup_missing" {
		return fail(fmt.Errorf("setup preparation is unavailable in state %s; %s", state.Status, state.Guidance))
	}
	loaded, err := config.Load(path)
	if err != nil {
		return fail(err)
	}
	d, err := loaded.Domain("alpha")
	if err != nil {
		return fail(err)
	}
	formatter := filepath.Join(filepath.Dir(path), "formatter")
	if _, err := os.Lstat(formatter); !errors.Is(err, os.ErrNotExist) {
		return fail(errors.New("formatter target already exists or is uncertain; inspect retained setup before retrying"))
	}
	if err := ctx.Err(); err != nil {
		return fail(err)
	}
	if state.Status == "domain_uninitialized" {
		if _, err := fmt.Fprintln(out, "Initializing the new alpha domain…"); err != nil {
			return fail(err)
		}
		if err := app.Run(ctx, []string{"--config", path, "--domain", "alpha", "domain", "init"}, publicOptions(io.Discard)); err != nil {
			return app.SetupInspection{}, true, errors.New("new domain initialization failed; inspect this configuration before retrying")
		}
	}
	if _, err := fmt.Fprintln(out, "Preparing packaged workspace helpers…"); err != nil {
		return app.SetupInspection{}, true, err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return app.SetupInspection{}, true, err
	}
	runCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	var observed bytes.Buffer
	command := execx.Command{Path: "/bin/bash", Args: []string{script, source, path, "alpha", input.PrebuiltResources, formatter}, Env: []string{"PATH=/usr/bin:/bin", "HOME=" + home, "LANG=C", "LC_ALL=C", "TMPDIR=" + os.TempDir()}}
	if _, err := (basebuild.OSOwnedScriptRunner{Output: io.MultiWriter(out, &observed)}).RunOwned(runCtx, command); err != nil {
		return app.SetupInspection{}, true, fmt.Errorf("packaged helper preparation failed; inspect retained setup: %w", err)
	}
	if observed.String() != "prepared packaged formatter artifacts: "+formatter+"\n" {
		return app.SetupInspection{}, true, errors.New("package helper returned an unexpected preparation receipt; inspect retained setup")
	}
	args := []string{"--config", path, "--domain", string(d.ID), "project", "setup", "--source-root", source, "--formatter-bundle", formatter, "--iso", input.ISOPath, "--prebuilt-resources", input.PrebuiltResources, "--openssl", input.OpenSSLPath, "--openssl-sha256", hex.EncodeToString(openDigest[:]), "--xorriso", input.XorrisoPath, "--xorriso-sha256", hex.EncodeToString(xorDigest[:])}
	if err := app.Run(ctx, args, publicOptions(out)); err != nil {
		return app.SetupInspection{}, true, fmt.Errorf("recording the new project setup failed; inspect retained setup: %w", err)
	}
	result := app.InspectSetup(ctx, path, setupOptions)
	if result.Status != "ready" {
		return result, true, fmt.Errorf("setup prerequisites remain %s; %s", result.Status, result.Guidance)
	}
	return result, true, ctx.Err()
}
