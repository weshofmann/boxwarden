package alphaprep

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/weshofmann/boxwarden/internal/backend"
	"github.com/weshofmann/boxwarden/internal/basebuild"
	"github.com/weshofmann/boxwarden/internal/config"
	"github.com/weshofmann/boxwarden/internal/execx"
	"github.com/weshofmann/boxwarden/internal/hostx"
)

// BuildComponents are the process and observer authorities supplied after
// Preflight. The seed tools are individually pinned because they are outside
// the Tart/Softnet host manifest.
type BuildComponents struct {
	Runner        execx.Runner
	Observer      backend.Observer
	ScriptRunner  basebuild.OwnedScriptRunner
	Launcher      basebuild.InstallerLauncher
	OpenSSLPath   string
	OpenSSLSHA256 string
	XorrisoPath   string
	XorrisoSHA256 string
}

// NewBuildDependencies binds an admitted runtime to the exact configured
// domain and verifies every host executable before a build reserves an
// attempt. Build and the individual adapters recheck their inputs at use.
func NewBuildDependencies(loaded config.Config, selected config.Domain, runtime hostx.RuntimeExpectation, components BuildComponents) (basebuild.Dependencies, error) {
	admitted, err := loaded.Domain(string(selected.ID))
	if err != nil || admitted != selected {
		return basebuild.Dependencies{}, errors.New("selected domain differs from admitted configuration")
	}
	host, err := loaded.HostAdmission()
	if err != nil {
		return basebuild.Dependencies{}, err
	}
	manifest := runtime.Manifest
	if manifest.Version != hostx.ManifestVersion || manifest.Tart.Path != host.Host.TartExecutable || manifest.TartHome != host.Host.TartHome || runtime.SoftnetBinDir == "" || filepath.Dir(manifest.Softnet.Path) != runtime.SoftnetBinDir {
		return basebuild.Dependencies{}, errors.New("qualified runtime differs from admitted host configuration")
	}
	if components.Runner == nil || components.Observer == nil || components.ScriptRunner == nil || components.Launcher == nil {
		return basebuild.Dependencies{}, errors.New("base builder process and observer authorities are required")
	}
	seed := basebuild.HostSeedBuilder{
		Runner: components.Runner, ScriptRunner: components.ScriptRunner,
		OpenSSLPath: components.OpenSSLPath, OpenSSLSHA256: components.OpenSSLSHA256,
		XorrisoPath: components.XorrisoPath, XorrisoSHA256: components.XorrisoSHA256,
	}
	if err := seed.CheckTools(); err != nil {
		return basebuild.Dependencies{}, fmt.Errorf("base builder tools: %w", err)
	}
	vm, err := basebuild.NewTartVM(basebuild.TartVMConfig{
		Runner: components.Runner, Observer: components.Observer,
		TartPath: manifest.Tart.Path, TartSHA256: manifest.Tart.ExecutableSHA256,
		TartHome: manifest.TartHome, SoftnetBinDir: runtime.SoftnetBinDir,
		OperatorHome: manifest.Operator.Home, OperatorName: manifest.Operator.Name,
		Launcher: components.Launcher,
	})
	if err != nil {
		return basebuild.Dependencies{}, fmt.Errorf("base builder VM: %w", err)
	}
	return basebuild.Dependencies{Checks: basebuild.RecipeChecks{}, Seed: seed, VM: vm}, nil
}
