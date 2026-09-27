package tart

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/weshofmann/boxwarden/internal/backend"
)

func TestLauncherUsesClosedQualifiedTartInvocation(t *testing.T) {
	generation := t.TempDir()
	if err := os.Chmod(generation, 0o700); err != nil {
		t.Fatal(err)
	}
	process := &recordingProcessStarter{handle: &processHandleFake{}}
	launcher := newLauncher(LaunchConfig{
		TartPath:      "/opt/qualified/tart",
		TartHome:      "/Users/wes/.boxwarden/tart",
		SoftnetBinDir: "/Library/Boxwarden/toolchains/softnet/0.19.0/digest",
		OperatorHome:  "/Users/wes",
		OperatorName:  "wes",
	}, process)
	request := backend.StartRequest{ObjectID: "boxwarden-work-dev", SerialDevice: "/dev/ttys004", GenerationDirectory: generation}

	handle, err := launcher.Start(context.Background(), request)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if handle == nil {
		t.Fatal("Start() returned no owned process handle")
	}
	wantArgs := []string{"run", "--net-softnet", "--no-audio", "--no-clipboard", "--serial-path", "/dev/ttys004", "boxwarden-work-dev"}
	if got := process.spec.path; got != "/opt/qualified/tart" {
		t.Fatalf("process path = %q, want configured absolute Tart path", got)
	}
	if got := process.spec.args; !sameLifecycleStrings(got, wantArgs) {
		t.Fatalf("process args = %#v, want %#v", got, wantArgs)
	}
	wantEnv := []string{
		"PATH=/Library/Boxwarden/toolchains/softnet/0.19.0/digest",
		"HOME=/Users/wes",
		"USER=wes",
		"LOGNAME=wes",
		"TART_HOME=/Users/wes/.boxwarden/tart",
		"TMPDIR=" + filepath.Join(generation, "tart"),
		"LANG=C",
		"LC_ALL=C",
	}
	if got := process.spec.env; !sameLifecycleStrings(got, wantEnv) {
		t.Fatalf("process environment = %#v, want exact closed environment %#v", got, wantEnv)
	}
	if got := process.spec.dir; got != filepath.Join(generation, "tart") {
		t.Fatalf("process directory = %q, want private Tart scratch", got)
	}
	if info, err := os.Lstat(filepath.Join(generation, "tart")); err != nil || !info.IsDir() || info.Mode().Perm() != 0o700 {
		t.Fatalf("private Tart scratch = %v, %v", info, err)
	}
}

func TestLauncherRefusesPreexistingTartScratch(t *testing.T) {
	generation := t.TempDir()
	if err := os.Chmod(generation, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(generation, "tart"), 0o700); err != nil {
		t.Fatal(err)
	}
	process := &recordingProcessStarter{handle: &processHandleFake{}}
	request := backend.StartRequest{ObjectID: "boxwarden-work-dev", SerialDevice: "/dev/ttys004", GenerationDirectory: generation}
	if _, err := newLauncher(validLaunchConfig(), process).Start(context.Background(), request); err == nil {
		t.Fatal("Start() accepted preexisting Tart scratch")
	}
	if process.started {
		t.Fatal("Tart process started with preexisting scratch")
	}
}

func TestTartScratchCleanupRequiresReapAndExactContents(t *testing.T) {
	generation := t.TempDir()
	if err := os.Chmod(generation, 0o700); err != nil {
		t.Fatal(err)
	}
	process := &recordingProcessStarter{handle: &controlledProcessHandle{reap: make(chan struct{})}}
	request := backend.StartRequest{ObjectID: "boxwarden-work-dev", SerialDevice: "/dev/ttys004", GenerationDirectory: generation}
	handle, err := newLauncher(validLaunchConfig(), process).Start(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	scratch := filepath.Join(generation, "tart")
	if err := os.WriteFile(filepath.Join(scratch, "foreign"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	finished := make(chan error, 1)
	go func() { finished <- handle.Wait(context.Background()) }()
	select {
	case <-finished:
		t.Fatal("scratch cleanup ran before Tart reap")
	default:
	}
	close(process.handle.(*controlledProcessHandle).reap)
	if err := <-finished; err == nil {
		t.Fatal("Wait() accepted unexpected scratch entry")
	}
	if _, err := os.Lstat(scratch); err != nil {
		t.Fatalf("unsafe scratch was removed: %v", err)
	}
}

func TestTartScratchCleanupRemovesOnlyOwnedSocketAfterReap(t *testing.T) {
	root, err := filepath.EvalSymlinks("/tmp")
	if err != nil {
		t.Fatal(err)
	}
	generation, err := os.MkdirTemp(root, "bw-tart-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(generation) })
	if err := os.Chmod(generation, 0o700); err != nil {
		t.Fatal(err)
	}
	process := &recordingProcessStarter{handle: &controlledProcessHandle{reap: make(chan struct{})}}
	request := backend.StartRequest{ObjectID: "boxwarden-work-dev", SerialDevice: "/dev/ttys004", GenerationDirectory: generation}
	handle, err := newLauncher(validLaunchConfig(), process).Start(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	scratch := filepath.Join(generation, "tart")
	socket := filepath.Join(scratch, "control.sock")
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: socket, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	listener.SetUnlinkOnClose(false)
	if err := os.Chmod(socket, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := handle.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(socket); err != nil {
		t.Fatalf("Stop removed Tart socket before reap: %v", err)
	}
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	close(process.handle.(*controlledProcessHandle).reap)
	if err := handle.Wait(context.Background()); err != nil {
		t.Fatalf("Wait() cleanup error = %v", err)
	}
	if _, err := os.Lstat(scratch); !os.IsNotExist(err) {
		t.Fatalf("scratch after exact reap = %v, want absent", err)
	}
}

type controlledProcessHandle struct{ reap chan struct{} }

func (*controlledProcessHandle) Stop(context.Context) error   { return nil }
func (h *controlledProcessHandle) Wait(context.Context) error { <-h.reap; return nil }

func TestLauncherRejectsAmbientOrUnqualifiedConfiguration(t *testing.T) {
	valid := validLaunchConfig()
	request := backend.StartRequest{ObjectID: "boxwarden-work-dev", SerialDevice: "/dev/ttys004", GenerationDirectory: "/private/runtime/work/dev/generation-1"}
	for name, configure := range map[string]func(*LaunchConfig){
		"relative Tart path":             func(c *LaunchConfig) { c.TartPath = "tart" },
		"root Tart home":                 func(c *LaunchConfig) { c.TartHome = "/" },
		"traversing Softnet path":        func(c *LaunchConfig) { c.SoftnetBinDir = "/Library/Boxwarden/../softnet" },
		"relative operator home":         func(c *LaunchConfig) { c.OperatorHome = "Users/wes" },
		"empty operator name":            func(c *LaunchConfig) { c.OperatorName = "" },
		"operator environment injection": func(c *LaunchConfig) { c.OperatorName = "wes=unsafe" },
	} {
		t.Run(name, func(t *testing.T) {
			config := valid
			configure(&config)
			starter := &recordingProcessStarter{handle: &processHandleFake{}}
			if _, err := newLauncher(config, starter).Start(context.Background(), request); err == nil {
				t.Fatal("Start() error = nil, want configuration refusal")
			}
			if starter.started {
				t.Fatalf("Start() started unqualified process %#v", starter.spec)
			}
		})
	}
}

type recordingProcessStarter struct {
	spec    processSpec
	handle  backend.Handle
	started bool
}

func (s *recordingProcessStarter) start(_ context.Context, spec processSpec) (backend.Handle, error) {
	s.started = true
	s.spec = spec
	return s.handle, nil
}

type processHandleFake struct{}

func (*processHandleFake) Stop(context.Context) error { return nil }
func (*processHandleFake) Wait(context.Context) error { return nil }

func validLaunchConfig() LaunchConfig {
	return LaunchConfig{
		TartPath:      "/opt/qualified/tart",
		TartHome:      "/Users/wes/.boxwarden/tart",
		SoftnetBinDir: "/Library/Boxwarden/toolchains/softnet/0.19.0/digest",
		OperatorHome:  "/Users/wes",
		OperatorName:  "wes",
	}
}

func sameLifecycleStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
