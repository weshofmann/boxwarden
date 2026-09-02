package tart

import (
	"context"
	"testing"

	"github.com/weshofmann/boxwarden/internal/backend"
)

func TestLauncherUsesOnlyQualifiedFixedTartLaunch(t *testing.T) {
	process := &recordingProcessStarter{handle: &processHandleFake{}}
	launcher := NewLauncher(process, LaunchConfig{
		TartPath:      "/opt/qualified/tart",
		SoftnetBinDir: "/Library/Boxwarden/toolchains/softnet/0.19.0",
		Home:          "/Users/wes",
		User:          "wes",
		Logname:       "wes",
		TartHome:      "/Users/wes/.boxwarden/tart",
	})
	request := backend.StartRequest{ObjectID: "boxwarden-work-dev", SerialDevice: "/dev/ttys004", GenerationDirectory: "/private/runtime/work/dev/generation-1"}

	handle, err := launcher.Start(context.Background(), request)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if handle == nil {
		t.Fatal("Start() handle = nil")
	}
	wantArgs := []string{"run", "--net-softnet", "--no-audio", "--no-clipboard", "--serial-path", "/dev/ttys004", "boxwarden-work-dev"}
	if got := process.spec; got.Path != "/opt/qualified/tart" || !sameStrings(got.Args, wantArgs) {
		t.Fatalf("launch = %#v, want exact qualified Tart argv %#v", got, wantArgs)
	}
	wantEnv := []string{
		"PATH=/Library/Boxwarden/toolchains/softnet/0.19.0",
		"HOME=/Users/wes",
		"USER=wes",
		"LOGNAME=wes",
		"TART_HOME=/Users/wes/.boxwarden/tart",
		"TMPDIR=/private/runtime/work/dev/generation-1",
		"LANG=C",
		"LC_ALL=C",
	}
	if !sameStrings(process.spec.Env, wantEnv) {
		t.Fatalf("launch env = %#v, want closed exact env %#v", process.spec.Env, wantEnv)
	}
}

func TestLauncherRejectsUnsafeRequestBeforeStartingProcess(t *testing.T) {
	for name, request := range map[string]backend.StartRequest{
		"object option":        {ObjectID: "--unsafe", SerialDevice: "/dev/ttys004", GenerationDirectory: "/private/runtime/generation"},
		"serial traversal":     {ObjectID: "boxwarden-work-dev", SerialDevice: "/dev/../tty", GenerationDirectory: "/private/runtime/generation"},
		"generation traversal": {ObjectID: "boxwarden-work-dev", SerialDevice: "/dev/ttys004", GenerationDirectory: "/private/runtime/../generation"},
	} {
		t.Run(name, func(t *testing.T) {
			process := &recordingProcessStarter{handle: &processHandleFake{}}
			launcher := NewLauncher(process, validLaunchConfig())
			if _, err := launcher.Start(context.Background(), request); err == nil {
				t.Fatal("Start() error = nil, want pre-start refusal")
			}
			if process.started {
				t.Fatalf("Start() invoked process for unsafe request: %#v", process.spec)
			}
		})
	}
}

type recordingProcessStarter struct {
	spec    ProcessSpec
	handle  backend.Handle
	started bool
}

func (s *recordingProcessStarter) Start(_ context.Context, spec ProcessSpec) (backend.Handle, error) {
	s.started, s.spec = true, spec
	return s.handle, nil
}

type processHandleFake struct{}

func (*processHandleFake) Stop(context.Context) error { return nil }
func (*processHandleFake) Wait(context.Context) error { return nil }

func validLaunchConfig() LaunchConfig {
	return LaunchConfig{TartPath: "/opt/qualified/tart", SoftnetBinDir: "/Library/Boxwarden/toolchains/softnet/0.19.0", Home: "/Users/wes", User: "wes", Logname: "wes", TartHome: "/Users/wes/.boxwarden/tart"}
}
