package tart

import (
	"context"
	"github.com/weshofmann/boxwarden/internal/backend"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func clipboardLaunchFixture(t *testing.T) (LaunchConfig, backend.StartRequest) {
	t.Helper()
	metadata := &ClipboardLaunchMetadata{CLIPath: "/opt/Boxwarden tools/boxwarden", ConfigPath: "/opt/Boxwarden tools/config.json", Domain: "work", SessionName: "dev", SessionID: "11111111-2222-4333-8444-555555555555", Generation: "00112233-4455-4677-8899-aabbccddeeff"}
	generation := filepath.Join(t.TempDir(), "runtime", metadata.Domain, metadata.SessionID, metadata.Generation)
	if err := os.MkdirAll(generation, 0700); err != nil {
		t.Fatal(err)
	}
	config := validLaunchConfig()
	config.Clipboard = metadata
	return config, backend.StartRequest{ObjectID: "actual-backend-object", SerialDevice: "/dev/ttys004", GenerationDirectory: generation}
}
func TestClipboardLauncherAddsExactMetadataAndPreservesIsolation(t *testing.T) {
	config, request := clipboardLaunchFixture(t)
	process := &recordingProcessStarter{handle: &processHandleFake{}}
	handle, err := newLauncher(config, process).Start(t.Context(), request)
	if err != nil || handle == nil {
		t.Fatal(err)
	}
	m := config.Clipboard
	want := []string{"run", "--net-softnet", "--no-audio", "--no-clipboard", "--serial-path", request.SerialDevice, "--boxwarden-clipboard-cli", m.CLIPath, "--boxwarden-clipboard-config", m.ConfigPath, "--boxwarden-clipboard-domain", m.Domain, "--boxwarden-clipboard-session", m.SessionName, "--boxwarden-clipboard-session-id", m.SessionID, "--boxwarden-clipboard-generation", m.Generation, request.ObjectID}
	if !reflect.DeepEqual(process.spec.args, want) {
		t.Fatalf("argv = %#v", process.spec.args)
	}
	env := []string{"PATH=" + config.SoftnetBinDir, "HOME=" + config.OperatorHome, "USER=" + config.OperatorName, "LOGNAME=" + config.OperatorName, "TART_HOME=" + config.TartHome, "TMPDIR=" + filepath.Join(request.GenerationDirectory, "tart"), "LANG=C", "LC_ALL=C"}
	if !reflect.DeepEqual(process.spec.env, env) {
		t.Fatal("closed environment changed")
	}
}
func TestClipboardLauncherLegacyArgumentsUnchanged(t *testing.T) {
	config, request := clipboardLaunchFixture(t)
	config.Clipboard = nil
	process := &recordingProcessStarter{handle: &processHandleFake{}}
	if _, err := newLauncher(config, process).Start(t.Context(), request); err != nil {
		t.Fatal(err)
	}
	want := []string{"run", "--net-softnet", "--no-audio", "--no-clipboard", "--serial-path", request.SerialDevice, request.ObjectID}
	if !reflect.DeepEqual(process.spec.args, want) {
		t.Fatalf("legacy argv %#v", process.spec.args)
	}
}
func TestClipboardLauncherRejectsInvalidMetadataBeforeChildOrScratch(t *testing.T) {
	cases := map[string]func(*ClipboardLaunchMetadata){"partial": func(m *ClipboardLaunchMetadata) { m.CLIPath = "" }, "relative CLI": func(m *ClipboardLaunchMetadata) { m.CLIPath = "boxwarden" }, "unclean config": func(m *ClipboardLaunchMetadata) { m.ConfigPath = "/opt/../config.json" }, "control path": func(m *ClipboardLaunchMetadata) { m.CLIPath = "/opt/boxwarden\n" }, "invalid domain": func(m *ClipboardLaunchMetadata) { m.Domain = "work-evil" }, "invalid session": func(m *ClipboardLaunchMetadata) { m.SessionName = "dev-evil" }, "invalid ID": func(m *ClipboardLaunchMetadata) { m.SessionID = "not-uuid" }, "invalid generation": func(m *ClipboardLaunchMetadata) { m.Generation = "not-uuid" }, "stale generation": func(m *ClipboardLaunchMetadata) { m.Generation = "10112233-4455-4677-8899-aabbccddeeff" }, "wrong domain": func(m *ClipboardLaunchMetadata) { m.Domain = "personal" }, "wrong ID": func(m *ClipboardLaunchMetadata) { m.SessionID = "21111111-2222-4333-8444-555555555555" }}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			config, request := clipboardLaunchFixture(t)
			mutate(config.Clipboard)
			process := &recordingProcessStarter{handle: &processHandleFake{}}
			handle, err := newLauncher(config, process).Start(context.Background(), request)
			if err == nil || handle != nil || process.started {
				t.Fatal("invalid metadata reached child")
			}
			if _, err := os.Lstat(filepath.Join(request.GenerationDirectory, "tart")); !os.IsNotExist(err) {
				t.Fatal("invalid metadata created scratch")
			}
		})
	}
}

func TestClipboardLauncherSnapshotsMetadataAtConstruction(t *testing.T) {
	config, request := clipboardLaunchFixture(t)
	original := *config.Clipboard
	process := &recordingProcessStarter{handle: &processHandleFake{}}
	launcher := newLauncher(config, process)
	config.Clipboard.CLIPath = "/replacement/boxwarden"
	config.Clipboard.Generation = "10112233-4455-4677-8899-aabbccddeeff"
	if _, err := launcher.Start(t.Context(), request); err != nil {
		t.Fatal(err)
	}
	if launcher.config.Clipboard == config.Clipboard || *launcher.config.Clipboard != original {
		t.Fatal("launch target changed through retained caller pointer")
	}
}
func TestClipboardLauncherRejectsEachOmittedMetadataField(t *testing.T) {
	for _, field := range []string{"CLIPath", "ConfigPath", "Domain", "SessionName", "SessionID", "Generation"} {
		t.Run(field, func(t *testing.T) {
			config, request := clipboardLaunchFixture(t)
			reflect.ValueOf(config.Clipboard).Elem().FieldByName(field).SetString("")
			process := &recordingProcessStarter{handle: &processHandleFake{}}
			if handle, err := newLauncher(config, process).Start(t.Context(), request); err == nil || handle != nil || process.started {
				t.Fatal("partial metadata reached child")
			}
		})
	}
}
