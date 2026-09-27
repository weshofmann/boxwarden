package exportx

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/weshofmann/boxwarden/internal/execx"
)

type inspectorSignatureRunner func(context.Context, execx.Command) (execx.Result, error)

func (run inspectorSignatureRunner) Run(ctx context.Context, command execx.Command) (execx.Result, error) {
	return run(ctx, command)
}

func TestInspectorBundleAdmissionBindsRequestInputsAndExactInitrd(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "bundle")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "casper"), 0o700); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{}
	write := func(name string, body []byte, mode os.FileMode) {
		t.Helper()
		path := filepath.Join(root, name)
		if err := os.WriteFile(path, body, mode); err != nil {
			t.Fatal(err)
		}
		hash := sha256.Sum256(body)
		files[name] = hex.EncodeToString(hash[:])
	}
	request := []byte(`{"transaction":"fixture"}`)
	original := []byte("synthetic compressed initrd")
	probe := []byte("synthetic guest probe")
	write("casper/vmlinuz", []byte("synthetic kernel"), 0o400)
	write("casper/initrd", original, 0o400)
	write("kernel-image", []byte("synthetic kernel image"), 0o600)
	write("alpha-probe", probe, 0o700)
	write("alpha-inspector", []byte("synthetic signed helper"), 0o700)
	write("request.json", request, 0o600)
	var packed bytes.Buffer
	packed.Write(original)
	packed.Write(bytes.Repeat([]byte{0}, (-len(original))&3))
	writeCPIOEntry(&packed, "alpha-probe", probe, 1, 0o100755)
	writeCPIOEntry(&packed, "alpha-export-request.json", request, 2, 0o100400)
	writeCPIOEntry(&packed, "TRAILER!!!", nil, 3, 0)
	write("inspector-initrd", packed.Bytes(), 0o600)
	source := inspectorSourceIdentity{files: map[string]string{"tools/alpha-inspector/guest/main.go": strings.Repeat("a", 64)}}
	pins := inspectorBundlePins{iso: strings.Repeat("b", 64), sourceKernel: files["casper/vmlinuz"], imageKernel: files["kernel-image"]}
	manifest := inspectorBundleManifest{Version: 1, SourceCommit: strings.Repeat("c", 40), SourceInputs: map[string]string{"tools/alpha-inspector/guest/main.go": source.files["tools/alpha-inspector/guest/main.go"]},
		ISOSHA256: pins.iso, Qualification: "production", Files: files,
		RunnerEntitlements: map[string]bool{"com.apple.security.virtualization": true}}
	writeManifest := func() {
		t.Helper()
		body, err := json.Marshal(manifest)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "manifest.json"), body, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	writeManifest()
	runner := inspectorSignatureRunner(func(_ context.Context, command execx.Command) (execx.Result, error) {
		if len(command.Env) != 3 || command.Env[0] != "PATH=/usr/bin:/bin" {
			return execx.Result{}, fmt.Errorf("unexpected signature environment")
		}
		switch {
		case command.Path == "/usr/bin/codesign" && len(command.Args) == 3 && command.Args[0] == "--verify":
			return execx.Result{}, nil
		case command.Path == "/usr/bin/codesign" && len(command.Args) == 4 && command.Args[0] == "-d":
			return execx.Result{Stdout: "synthetic plist"}, nil
		case command.Path == "/usr/bin/plutil" && string(command.Stdin) == "synthetic plist":
			return execx.Result{Stdout: `{"com.apple.security.virtualization":true}`}, nil
		default:
			return execx.Result{}, fmt.Errorf("unexpected signature command %s %v", command.Path, command.Args)
		}
	})
	admit := func(requestBytes []byte) error {
		_, err := admitInspectorBundle(context.Background(), root, requestBytes, source, runner, pins)
		return err
	}
	if err := admit(request); err != nil {
		t.Fatalf("valid bundle rejected: %v", err)
	}
	if err := admit([]byte("different journal request")); err == nil {
		t.Fatal("bundle admitted against different request")
	}
	manifest.SourceInputs["tools/alpha-inspector/guest/main.go"] = strings.Repeat("d", 64)
	writeManifest()
	if err := admit(request); err == nil {
		t.Fatal("bundle admitted against changed executable inputs")
	}
	manifest.SourceInputs = source.files
	writeManifest()
	packed.WriteByte('x')
	write("inspector-initrd", packed.Bytes(), 0o600)
	manifest.Files["inspector-initrd"] = files["inspector-initrd"]
	writeManifest()
	if err := admit(request); err == nil {
		t.Fatal("bundle admitted with extra initrd bytes")
	}
}

func TestInspectorJSONRejectsNestedDuplicatesAndTrailingValues(t *testing.T) {
	for _, raw := range []string{
		`{"files":{"a":"one","a":"two"}}`,
		`{"runner_entitlements":{"com.apple.security.virtualization":true,"com.apple.security.virtualization":false}}`,
		`{"version":1} {"version":2}`,
	} {
		if err := rejectDuplicateBundleJSON([]byte(raw)); err == nil {
			t.Fatalf("ambiguous JSON admitted: %s", raw)
		}
	}
}

func TestProductionInspectorBundleAdmission(t *testing.T) {
	bundle := os.Getenv("BOXWARDEN_TEST_INSPECTOR_BUNDLE")
	if bundle == "" {
		t.Skip("private production bundle path not supplied")
	}
	source := os.Getenv("BOXWARDEN_TEST_INSPECTOR_SOURCE")
	if source == "" {
		t.Fatal("clean inspector source checkout path is required")
	}
	request, err := os.ReadFile(filepath.Join(bundle, "request.json"))
	if err != nil {
		t.Fatal(err)
	}
	admitted, err := AdmitInspectorBundle(context.Background(), bundle, source, request)
	if err != nil {
		t.Fatal(err)
	}
	if admitted.Helper != filepath.Join(bundle, "alpha-inspector") || admitted.Kernel != filepath.Join(bundle, "kernel-image") || admitted.Initrd != filepath.Join(bundle, "inspector-initrd") {
		t.Fatalf("unexpected admitted launch inputs: %+v", admitted)
	}
}
