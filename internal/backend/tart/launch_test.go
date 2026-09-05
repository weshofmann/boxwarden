package tart

import (
	"context"
	"testing"

	"github.com/weshofmann/boxwarden/internal/backend"
)

func TestLauncherUsesClosedQualifiedTartInvocation(t *testing.T) {
	process := &recordingProcessStarter{handle: &processHandleFake{}}
	launcher := newLauncher(LaunchConfig{
		TartPath:      "/opt/qualified/tart",
		TartHome:      "/Users/wes/.boxwarden/tart",
		SoftnetBinDir: "/Library/Boxwarden/toolchains/softnet/0.19.0/digest",
		OperatorHome:  "/Users/wes",
		OperatorName:  "wes",
	}, process)
	request := backend.StartRequest{ObjectID: "boxwarden-work-dev", SerialDevice: "/dev/ttys004", GenerationDirectory: "/private/runtime/work/dev/generation-1"}

	handle, err := launcher.Start(context.Background(), request)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if handle != process.handle {
		t.Fatalf("Start() handle = %#v, want owned process handle %#v", handle, process.handle)
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
		"TMPDIR=/private/runtime/work/dev/generation-1",
		"LANG=C",
		"LC_ALL=C",
	}
	if got := process.spec.env; !sameLifecycleStrings(got, wantEnv) {
		t.Fatalf("process environment = %#v, want exact closed environment %#v", got, wantEnv)
	}
	if got := process.spec.dir; got != request.GenerationDirectory {
		t.Fatalf("process directory = %q, want generation directory %q", got, request.GenerationDirectory)
	}
}

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
