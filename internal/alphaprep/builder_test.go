package alphaprep

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/weshofmann/boxwarden/internal/backend/fake"
	"github.com/weshofmann/boxwarden/internal/basebuild"
	"github.com/weshofmann/boxwarden/internal/execx"
	"github.com/weshofmann/boxwarden/internal/hostx"
)

type fixtureToolProbeRunner struct{}

func (fixtureToolProbeRunner) Run(_ context.Context, command execx.Command) (execx.Result, error) {
	if filepath.Base(command.Path) == "openssl" && strings.Join(command.Args, "|") == "passwd|-6|-stdin" {
		return execx.Result{Stdout: "$6$abcdefghijklmnop$" + strings.Repeat("A", 86) + "\n"}, nil
	}
	if filepath.Base(command.Path) == "xorriso" && strings.Join(command.Args, "|") == "-version" {
		return execx.Result{Stdout: "GNU xorriso\nxorriso version : 1.5.8\n"}, nil
	}
	return execx.Result{}, fmt.Errorf("unexpected tool probe: %s %v", command.Path, command.Args)
}

func buildComponentsFixture(t *testing.T) (BuildComponents, hostx.RuntimeExpectation) {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	writeTool := func(name string) (string, string) {
		path := filepath.Join(root, name)
		data := []byte("fixture " + name)
		if err := os.WriteFile(path, data, 0700); err != nil {
			t.Fatal(err)
		}
		return path, fmt.Sprintf("%x", sha256.Sum256(data))
	}
	tart, tartHash := writeTool("tart")
	openssl, opensslHash := writeTool("openssl")
	xorriso, xorrisoHash := writeTool("xorriso")
	components := BuildComponents{Runner: fixtureToolProbeRunner{}, Observer: fake.New(), ScriptRunner: basebuild.OSOwnedScriptRunner{}, Launcher: basebuild.OSInstallerLauncher{}, OpenSSLPath: openssl, OpenSSLSHA256: opensslHash, XorrisoPath: xorriso, XorrisoSHA256: xorrisoHash}
	runtime := hostx.RuntimeExpectation{Manifest: hostx.Manifest{Version: hostx.ManifestVersion, Tart: hostx.ToolIdentity{Path: tart, ExecutableSHA256: tartHash}, Softnet: hostx.ToolIdentity{Path: "/qualified/softnet/softnet"}, Operator: hostx.Operator{Name: "operator", Home: root}, TartHome: filepath.Join(root, "tart-home")}, SoftnetBinDir: "/qualified/softnet"}
	return components, runtime
}

func TestNewBuildDependenciesBindsExactRuntimeAndChecksSeedTools(t *testing.T) {
	loaded, selected := preflightFixture(t)
	components, runtime := buildComponentsFixture(t)
	admission, err := loaded.HostAdmission()
	if err != nil {
		t.Fatal(err)
	}
	runtime.Manifest.Tart.Path = admission.Host.TartExecutable
	tartData, err := os.ReadFile(admission.Host.TartExecutable)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(admission.Host.TartExecutable, 0700); err != nil {
		t.Fatal(err)
	}
	runtime.Manifest.Tart.ExecutableSHA256 = fmt.Sprintf("%x", sha256.Sum256(tartData))
	runtime.Manifest.TartHome = admission.Host.TartHome
	deps, err := NewBuildDependencies(loaded, selected, runtime, components)
	if err != nil || deps.Checks == nil || deps.Seed == nil || deps.VM == nil {
		t.Fatalf("build composition = %+v, %v", deps, err)
	}
	if _, ok := deps.VM.(basebuild.CandidateIdentity); !ok {
		t.Fatal("composed VM cannot bind prepared cache to exact candidate identity")
	}
	if err := os.WriteFile(components.XorrisoPath, []byte("drifted"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := NewBuildDependencies(loaded, selected, runtime, components); err == nil {
		t.Fatal("drifted remaster tool admitted before build attempt")
	}
}

func TestNewBuildDependenciesRejectsRuntimeOrDomainSubstitution(t *testing.T) {
	loaded, selected := preflightFixture(t)
	components, runtime := buildComponentsFixture(t)
	selected.StateRoot += "-other"
	if _, err := NewBuildDependencies(loaded, selected, runtime, components); err == nil {
		t.Fatal("substituted domain admitted")
	}
	selected, _ = loaded.Domain("work")
	if _, err := NewBuildDependencies(loaded, selected, runtime, components); err == nil {
		t.Fatal("Tart path from another host admitted")
	}
}
