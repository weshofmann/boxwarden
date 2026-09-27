package sessionruntime

import (
	"context"
	"github.com/weshofmann/boxwarden/internal/backend/tart"
	"github.com/weshofmann/boxwarden/internal/hostx"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestOwnerClipboardMetadataOnlyForQualifiedStagedTart(t *testing.T) {
	for _, staged := range []bool{false, true} {
		t.Run(map[bool]string{false: "upstream", true: "controlled"}[staged], func(t *testing.T) {
			f := newFixture(t)
			originalHost := f.owner.deps.host
			f.owner.deps.host = hostFunc(func(ctx context.Context, r hostx.Request) (hostx.RuntimeExpectation, error) {
				expectation, err := originalHost.CheckRuntime(ctx, r)
				if staged {
					expectation.Manifest.Tart = hostx.ToolIdentity{Path: f.tartPath, Version: hostx.ControlledClipboardTartVersion, ExecutableSHA256: hostx.ControlledClipboardTartExecutableSHA256, ArchiveSHA256: hostx.ControlledClipboardTartArchiveSHA256}
				} else {
					expectation.Manifest.Tart = hostx.ToolIdentity{Path: f.tartPath, Version: hostx.TartVersion, ExecutableSHA256: hostx.TartExecutableSHA256, ArchiveSHA256: hostx.TartArchiveSHA256}
				}
				return expectation, err
			})
			if err := f.owner.Start(t.Context(), f.request); err != nil {
				t.Fatal(err)
			}
			if !staged {
				if f.launchConfig.Clipboard != nil {
					t.Fatal("legacy upstream received unsupported flags")
				}
				return
			}
			executable, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			executable, err = filepath.EvalSymlinks(executable)
			if err != nil {
				t.Fatal(err)
			}
			expected := &tart.ClipboardLaunchMetadata{CLIPath: executable, ConfigPath: f.request.HostConfigPath, Domain: f.request.Binding.Domain, SessionName: f.request.SessionRecordName, SessionID: f.request.Binding.SessionID, Generation: f.request.Binding.Generation}
			if !reflect.DeepEqual(f.launchConfig.Clipboard, expected) {
				t.Fatalf("metadata=%+v expected=%+v", f.launchConfig.Clipboard, expected)
			}
			if f.startRequest.ObjectID != f.request.Binding.BackendObject || f.startRequest.GenerationDirectory != f.request.RuntimeDirectory {
				t.Fatal("changed generic start request identity")
			}
		})
	}
}
