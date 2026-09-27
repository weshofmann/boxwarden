package basebuild

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/weshofmann/boxwarden/internal/backend"
)

type fakeInstallerSerial struct {
	endpoint string
	closed   int
	waits    []string
	lines    []string
}

func (f *fakeInstallerSerial) TartSlave() string { return f.endpoint }
func (f *fakeInstallerSerial) InstallerWaitFor(_ context.Context, marker string) error {
	f.waits = append(f.waits, marker)
	return nil
}
func (f *fakeInstallerSerial) InstallerSendLine(_ context.Context, line string) error {
	f.lines = append(f.lines, line)
	return nil
}
func (f *fakeInstallerSerial) Close() error { f.closed++; return nil }

type fakeInstallerProcess struct {
	stopped int
	waited  int
	waitErr error
}

func (f *fakeInstallerProcess) Stop(context.Context) error { f.stopped++; return nil }
func (f *fakeInstallerProcess) Wait(context.Context) error { f.waited++; return f.waitErr }

func installerLaunchFixture(t *testing.T) InstallerLaunch {
	t.Helper()
	usePortableACLFixture(t)
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	iso := filepath.Join(dir, "installer.iso")
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
	return InstallerLaunch{Request: InstallerRequest{CandidateID: "bw-v02-build-r1", RunID: "run-0123456789ab", ISOPath: iso, SerialDirectory: filepath.Join(dir, "serial")}, TartPath: "/opt/homebrew/bin/tart", TartHome: filepath.Join(dir, "tart-home"), SoftnetBinDir: "/Library/Boxwarden/softnet", OperatorHome: "/Users/devel", OperatorName: "devel"}
}

func TestInstallerLauncherUsesExactReadOnlyISOAndOwnedSerial(t *testing.T) {
	launch := installerLaunchFixture(t)
	serial := &fakeInstallerSerial{endpoint: filepath.Join(launch.Request.SerialDirectory, "tart-serial")}
	process := &fakeInstallerProcess{}
	var spec installerProcessSpec
	handle, err := startInstaller(context.Background(), launch, func(_ context.Context, dir, runID string) (installerSerial, error) {
		if dir != filepath.Dir(launch.Request.ISOPath) {
			t.Fatalf("serial dir = %q", dir)
		}
		if runID != launch.Request.RunID {
			t.Fatalf("run ID = %q", runID)
		}
		return serial, nil
	}, func(_ context.Context, got installerProcessSpec) (backend.Handle, error) {
		spec = got
		return process, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"run", "--net-softnet", "--no-audio", "--no-clipboard", "--serial-path", serial.endpoint, "--disk=" + launch.Request.ISOPath + ":ro", launch.Request.CandidateID}
	if strings.Join(spec.args, "|") != strings.Join(want, "|") || spec.path != launch.TartPath || spec.dir != filepath.Dir(launch.Request.ISOPath) {
		t.Fatalf("installer child argv/path/dir = %+v", spec)
	}
	if strings.Join(spec.env, "|") != "PATH="+launch.SoftnetBinDir+"|HOME="+launch.OperatorHome+"|USER=devel|LOGNAME=devel|TART_HOME="+launch.TartHome+"|TMPDIR="+spec.dir+"|LANG=C|LC_ALL=C" {
		t.Fatalf("installer env = %q", spec.env)
	}
	if err := handle.WaitFor(context.Background(), InstalledPrompt(launch.Request.RunID)); err != nil {
		t.Fatal(err)
	}
	if err := handle.SendLine(context.Background(), FinalizerCommand); err != nil {
		t.Fatal(err)
	}
	if err := handle.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := handle.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	if process.stopped != 1 || process.waited != 1 || serial.closed != 1 || len(serial.waits) != 1 || len(serial.lines) != 1 {
		t.Fatalf("ownership lifecycle process=%+v serial=%+v", process, serial)
	}
}

func TestInstallerLauncherPreservesSerialOnUnprovenReap(t *testing.T) {
	launch := installerLaunchFixture(t)
	serial := &fakeInstallerSerial{endpoint: filepath.Join(launch.Request.SerialDirectory, "tart-serial")}
	process := &fakeInstallerProcess{waitErr: ErrScriptReapUnproven}
	handle, err := startInstaller(context.Background(), launch, func(context.Context, string, string) (installerSerial, error) { return serial, nil }, func(context.Context, installerProcessSpec) (backend.Handle, error) { return process, nil })
	if err != nil {
		t.Fatal(err)
	}
	if err := handle.Wait(context.Background()); !errors.Is(err, ErrScriptReapUnproven) {
		t.Fatalf("unproven wait = %v", err)
	}
	if serial.closed != 0 {
		t.Fatal("serial removed while Tart reap was unproven")
	}
}

func TestInstallerLauncherRejectsUnexpectedSerialAndAbsentProcess(t *testing.T) {
	launch := installerLaunchFixture(t)
	serial := &fakeInstallerSerial{endpoint: "/private/other/tart-serial"}
	called := false
	_, err := startInstaller(context.Background(), launch, func(context.Context, string, string) (installerSerial, error) { return serial, nil }, func(context.Context, installerProcessSpec) (backend.Handle, error) { called = true; return nil, nil })
	if err == nil || called || serial.closed != 1 {
		t.Fatalf("unexpected serial accepted: err=%v called=%t closed=%d", err, called, serial.closed)
	}
	serial = &fakeInstallerSerial{endpoint: filepath.Join(launch.Request.SerialDirectory, "tart-serial")}
	_, err = startInstaller(context.Background(), launch, func(context.Context, string, string) (installerSerial, error) { return serial, nil }, func(context.Context, installerProcessSpec) (backend.Handle, error) { return nil, nil })
	if err == nil || serial.closed != 1 {
		t.Fatalf("missing process owner accepted: err=%v closed=%d", err, serial.closed)
	}
}

func TestInstallerLauncherRejectsAmbiguousColonOperandsBeforeSerialCreation(t *testing.T) {
	launch := installerLaunchFixture(t)
	called := false
	serial := func(context.Context, string, string) (installerSerial, error) {
		called = true
		return nil, nil
	}
	start := func(context.Context, installerProcessSpec) (backend.Handle, error) {
		called = true
		return nil, nil
	}
	launch.SoftnetBinDir = "/Library/Boxwarden/softnet:/bin"
	if _, err := startInstaller(context.Background(), launch, serial, start); err == nil || called {
		t.Fatalf("ambiguous PATH accepted: err=%v called=%t", err, called)
	}
	launch.SoftnetBinDir = "/Library/Boxwarden/softnet"
	launch.Request.ISOPath = "/private/attempt:other/installer.iso"
	launch.Request.SerialDirectory = "/private/attempt:other/serial"
	if _, err := startInstaller(context.Background(), launch, serial, start); err == nil || called {
		t.Fatalf("ambiguous Tart disk operand accepted: err=%v called=%t", err, called)
	}
}
