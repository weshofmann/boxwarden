package workspaceformat

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/weshofmann/boxwarden/internal/execx"
)

type vzRunnerFunc func(context.Context, execx.Command) (execx.Result, error)

func (f vzRunnerFunc) Run(ctx context.Context, command execx.Command) (execx.Result, error) {
	return f(ctx, command)
}

func vzFixtureBundle(t *testing.T, root string) (string, formatterPins) {
	t.Helper()
	bundle := filepath.Join(t.TempDir(), "bundle")
	if err := os.Mkdir(bundle, 0o700); err != nil {
		t.Fatal(err)
	}
	pins := formatterPins{iso: strings.Repeat("1", 64), deb: strings.Repeat("2", 64)}
	files := make(map[string]string)
	for name, body := range map[string]string{
		"kernel-image": "kernel", "formatter-initrd": "initrd", "alpha-formatter": "guest",
		"e2fsck.static": "checker", "alpha-formatter-host": "signed-runner",
		"binding.swift": managedBindingSource(root, "work"),
	} {
		mode := os.FileMode(0o600)
		if name == "alpha-formatter-host" || name == "alpha-formatter" {
			mode = 0o700
		}
		if err := os.WriteFile(filepath.Join(bundle, name), []byte(body), mode); err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256([]byte(body))
		files[name] = hex.EncodeToString(sum[:])
	}
	pins.kernel, pins.checker = files["kernel-image"], files["e2fsck.static"]
	manifest := vzBundleDocument{Version: 1, SourceCommit: strings.Repeat("a", 40), ISOSHA256: pins.iso,
		CheckerDebSHA256: pins.deb, Files: files, ManagedStateRoot: root, ManagedDomain: "work",
		RunnerEntitlements: map[string]bool{"com.apple.security.virtualization": true}}
	raw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bundle, "manifest.json"), append(raw, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	return bundle, pins
}

func TestVZFormatterRunsExactManagedRequestAndReapsBeforeReturning(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state with spaces")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	bundle, pins := vzFixtureBundle(t, root)
	var commands []execx.Command
	formatter := VZFormatter{StateRoot: root, Domain: "work", BundlePath: bundle, pins: pins}
	formatter.runner = vzRunnerFunc(func(_ context.Context, command execx.Command) (execx.Result, error) {
		commands = append(commands, command)
		switch command.Path {
		case "/usr/bin/codesign":
			if reflect.DeepEqual(command.Args[:2], []string{"-d", "--entitlements"}) {
				return execx.Result{Stdout: "<plist version=\"1.0\"><dict><key>com.apple.security.virtualization</key><true/></dict></plist>"}, nil
			}
			return execx.Result{}, nil
		case "/usr/bin/plutil":
			return execx.Result{Stdout: `{"com.apple.security.virtualization":true}`}, nil
		case filepath.Join(bundle, "alpha-formatter-host"):
			if len(command.Args) != 10 || command.Args[0] != "run-managed" ||
				command.Args[3] != filepath.Join(root, "volumes", testVolumeID+".raw") ||
				command.Args[8] != testFSUUID || len(command.Args[9]) != 64 {
				t.Fatalf("managed runner argv lost exact element boundaries: %#v", command.Args)
			}
			if err := writeExt4Header(command.Args[3], testFSUUID); err != nil {
				return execx.Result{}, err
			}
			return execx.Result{Stdout: fmt.Sprintf("{\"version\":1,\"transaction\":%q,\"vm_stopped\":true,\"runtime_network_devices\":0,\"storage_devices\":1,\"storage_read_only\":false,\"serial_ports\":2,\"socket_devices\":0,\"shared_directory_devices\":0,\"observed_uuid\":%q,\"whole_device\":true,\"filesystem_clean\":true}\n", command.Args[7], testFSUUID)}, nil
		default:
			t.Fatalf("unexpected command: %+v", command)
			return execx.Result{}, nil
		}
	})
	qualified, err := Create(t.Context(), root, testRequest(), formatter)
	if err != nil {
		t.Fatal(err)
	}
	if qualified.FilesystemUUID != testFSUUID || len(commands) != 4 {
		t.Fatalf("qualified=%+v, commands=%d", qualified, len(commands))
	}
	for _, command := range commands {
		if !reflect.DeepEqual(command.Env, []string{"PATH=/usr/bin:/bin", "LANG=C", "LC_ALL=C"}) {
			t.Fatalf("formatter command environment is not closed: %#v", command.Env)
		}
	}
}

func TestVZFormatterRejectsModifiedBundleBeforeRunner(t *testing.T) {
	root := testRoot(t)
	bundle, pins := vzFixtureBundle(t, root)
	if err := os.WriteFile(filepath.Join(bundle, "formatter-initrd"), []byte("modified"), 0o600); err != nil {
		t.Fatal(err)
	}
	called := false
	formatter := VZFormatter{StateRoot: root, Domain: "work", BundlePath: bundle, pins: pins,
		runner: vzRunnerFunc(func(context.Context, execx.Command) (execx.Result, error) {
			called = true
			return execx.Result{}, nil
		})}
	if _, err := Create(t.Context(), root, testRequest(), formatter); err == nil || called {
		t.Fatalf("modified initrd reached external command: err=%v called=%v", err, called)
	}
}

func TestVZFormatterRejectsRunnerThatDoesNotProveVMStopped(t *testing.T) {
	root := testRoot(t)
	bundle, pins := vzFixtureBundle(t, root)
	formatter := VZFormatter{StateRoot: root, Domain: "work", BundlePath: bundle, pins: pins}
	formatter.runner = vzRunnerFunc(func(_ context.Context, command execx.Command) (execx.Result, error) {
		switch command.Path {
		case "/usr/bin/codesign":
			if command.Args[0] == "-d" {
				return execx.Result{Stdout: "<plist version=\"1.0\"><dict><key>com.apple.security.virtualization</key><true/></dict></plist>"}, nil
			}
			return execx.Result{}, nil
		case "/usr/bin/plutil":
			return execx.Result{Stdout: `{"com.apple.security.virtualization":true}`}, nil
		case filepath.Join(bundle, "alpha-formatter-host"):
			if err := writeExt4Header(command.Args[3], testFSUUID); err != nil {
				return execx.Result{}, err
			}
			return execx.Result{Stdout: fmt.Sprintf("{\"version\":1,\"transaction\":%q,\"vm_stopped\":false,\"runtime_network_devices\":0,\"storage_devices\":1,\"storage_read_only\":false,\"serial_ports\":2,\"socket_devices\":0,\"shared_directory_devices\":0,\"observed_uuid\":%q,\"whole_device\":true,\"filesystem_clean\":true}\n", command.Args[7], testFSUUID)}, nil
		default:
			t.Fatalf("unexpected command: %+v", command)
			return execx.Result{}, nil
		}
	})
	if _, err := Create(t.Context(), root, testRequest(), formatter); err == nil {
		t.Fatal("non-stopped formatter report qualified a disk")
	}
	journal, err := ReadJournal(root, testRequest())
	if err != nil || journal.State != StateFailed {
		t.Fatalf("non-stopped result did not retain failed journal: %+v, %v", journal, err)
	}
}
