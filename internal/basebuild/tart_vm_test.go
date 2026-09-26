package basebuild

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/weshofmann/boxwarden/internal/backend"
	"github.com/weshofmann/boxwarden/internal/execx"
)

type fakeTartObserver struct{ calls int }

func (f *fakeTartObserver) Observe(_ context.Context, id string) (backend.Observation, error) {
	f.calls++
	return backend.Observation{ObjectID: id, State: backend.ObjectUnknown}, nil
}

type fakeInstallerLauncher struct {
	calls   int
	request InstallerLaunch
}

func (f *fakeInstallerLauncher) StartInstaller(_ context.Context, request InstallerLaunch) (InstallerHandle, error) {
	f.calls++
	f.request = request
	events := []string{}
	return &fakeRun{events: &events}, nil
}

func TestTartVMUsesExactQualifiedCommandsAndLauncherBoundary(t *testing.T) {
	usePortableACLFixture(t)
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	tartPath, digest := seedTool(t, root, "tart")
	tartHome := filepath.Join(root, "tart-home")
	if err := os.Mkdir(tartHome, 0700); err != nil {
		t.Fatal(err)
	}
	runner := &seedCommandRunner{result: execx.Result{Stdout: "[]"}}
	observer := &fakeTartObserver{}
	launcher := &fakeInstallerLauncher{}
	vm, err := NewTartVM(TartVMConfig{Runner: runner, Observer: observer, TartPath: tartPath, TartSHA256: digest, TartHome: tartHome, SoftnetBinDir: "/Library/Boxwarden/softnet", OperatorHome: "/Users/devel", OperatorName: "devel", Launcher: launcher})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := vm.Observe(context.Background(), "bw-v02-build-r1"); err != nil {
		t.Fatal(err)
	}
	if err := vm.Create(context.Background(), CreateRequest{CandidateID: "bw-v02-build-r1", DiskGiB: 40}); err != nil {
		t.Fatal(err)
	}
	if err := vm.Configure(context.Background(), ConfigureRequest{CandidateID: "bw-v02-build-r1", CPUs: 4, MemoryMiB: 4096}); err != nil {
		t.Fatal(err)
	}
	if observer.calls != 1 {
		t.Fatalf("observer calls = %d", observer.calls)
	}
	if len(runner.commands) != 2 {
		t.Fatalf("command count = %d", len(runner.commands))
	}
	wants := []string{"create|bw-v02-build-r1|--linux|--disk-size|40", "set|bw-v02-build-r1|--cpu|4|--memory|4096"}
	for i, command := range runner.commands {
		if command.Path != tartPath || strings.Join(command.Args, "|") != wants[i] || strings.Join(command.Env, "|") != "PATH=/usr/bin:/bin|TART_HOME="+tartHome+"|LANG=C|LC_ALL=C" {
			t.Fatalf("command %d differs: path=%q argv=%q env=%q", i, command.Path, command.Args, command.Env)
		}
	}
	attempt := filepath.Join(root, "attempt")
	if err := os.Mkdir(attempt, 0700); err != nil {
		t.Fatal(err)
	}
	iso := filepath.Join(attempt, "installer.iso")
	file, err := os.OpenFile(iso, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(3540000000); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	_, err = vm.RunInstaller(context.Background(), InstallerRequest{CandidateID: "bw-v02-build-r1", RunID: "run-1", ISOPath: iso, SerialDirectory: filepath.Join(attempt, "serial")})
	if err != nil {
		t.Fatal(err)
	}
	if launcher.calls != 1 || launcher.request.Request.CandidateID != "bw-v02-build-r1" || launcher.request.Request.ISOPath != iso || launcher.request.TartPath != tartPath || launcher.request.TartHome != tartHome {
		t.Fatalf("launcher seam incorrect: %+v", launcher.request)
	}
}

func TestTartVMRejectsMissingInstallerLauncherAndDigestDrift(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	tartPath, digest := seedTool(t, root, "tart")
	config := TartVMConfig{Runner: &seedCommandRunner{}, Observer: &fakeTartObserver{}, TartPath: tartPath, TartSHA256: digest, TartHome: root, SoftnetBinDir: "/Library/Boxwarden/softnet", OperatorHome: "/Users/devel", OperatorName: "devel"}
	if _, err := NewTartVM(config); err == nil {
		t.Fatal("missing owned installer launcher accepted")
	}
	config.Launcher = &fakeInstallerLauncher{}
	config.TartSHA256 = strings.Repeat("0", 64)
	if _, err := NewTartVM(config); err == nil {
		t.Fatal("Tart digest drift accepted")
	}
}
