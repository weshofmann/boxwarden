package exportx

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
)

func TestCaptureInspectorHelperProcess(t *testing.T) {
	if len(os.Args) < 4 || os.Args[2] != "capture-helper" {
		return
	}
	switch os.Args[3] {
	case "ok":
		_, _ = io.WriteString(os.Stdout, "BWEX test bytes")
		_, _ = io.WriteString(os.Stderr, "BOOT_EVIDENCE {\"console_bytes\":10,\"export_bytes\":15,\"runtime_network_devices\":0,\"vm_state\":\"stopped\"}\n")
	case "export":
		_, _ = io.WriteString(os.Stdout, "BWEX test bytes")
		_, _ = io.WriteString(os.Stderr, "BOOT_EVIDENCE {\"console_bytes\":10,\"export_bytes\":15,\"runtime_network_devices\":0,\"vm_state\":\"stopped\",\"inspector_mode\":\"export\"}\n")
	case "overflow":
		_, _ = io.WriteString(os.Stdout, strings.Repeat("x", 64))
		_, _ = io.WriteString(os.Stderr, "BOOT_EVIDENCE {\"console_bytes\":0,\"export_bytes\":64,\"runtime_network_devices\":0,\"vm_state\":\"stopped\"}\n")
	case "running":
		_, _ = io.WriteString(os.Stdout, "BWEX test bytes")
		_, _ = io.WriteString(os.Stderr, "BOOT_EVIDENCE {\"console_bytes\":10,\"export_bytes\":15,\"runtime_network_devices\":0,\"vm_state\":\"running\"}\n")
	case "duplicate":
		_, _ = io.WriteString(os.Stdout, "BWEX test bytes")
		_, _ = io.WriteString(os.Stderr, "BOOT_EVIDENCE {\"console_bytes\":10,\"export_bytes\":15,\"export_bytes\":15,\"runtime_network_devices\":0,\"vm_state\":\"stopped\"}\n")
	case "exit":
		os.Exit(7)
	default:
		os.Exit(8)
	}
	os.Exit(0)
}

func TestCaptureExportInspectorRequiresExportModeAndRemovesSyntheticSpool(t *testing.T) {
	for _, tc := range []struct {
		mode string
		pass bool
	}{{"ok", false}, {"export", true}} {
		t.Run(tc.mode, func(t *testing.T) {
			parent := t.TempDir()
			if err := os.Chmod(parent, 0o700); err != nil {
				t.Fatal(err)
			}
			result, err := CaptureExportInspector(context.Background(), os.Args[0], []string{"-test.run=^TestCaptureInspectorHelperProcess$", "capture-helper", tc.mode}, parent)
			if (err == nil) != tc.pass {
				t.Fatalf("capture export = %+v, %v", result, err)
			}
			if !tc.pass {
				if result.Stream != nil {
					t.Fatal("synthetic helper exposed an export stream")
				}
				if _, statErr := os.Lstat(parent + "/stream.bin"); !os.IsNotExist(statErr) {
					t.Fatalf("synthetic spool survived rejection: %v", statErr)
				}
				return
			}
			if result.Evidence.Mode != "export" || result.Stream == nil {
				t.Fatalf("export evidence missing: %+v", result.Evidence)
			}
			if err := result.AdmitForPublication(parent); err != nil {
				t.Fatalf("captured spool failed exact publication admission: %v", err)
			}
			if err := result.AdmitForPublication(t.TempDir()); err == nil {
				t.Fatal("captured spool admitted under another parent")
			}
			if err := result.Remove(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestCaptureInspectorRequiresStoppedZeroNICReapedHelperBeforeStream(t *testing.T) {
	for _, scenario := range []struct {
		name    string
		wantErr bool
	}{
		{"ok", false}, {"overflow", true}, {"running", true}, {"duplicate", true}, {"exit", true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			parent := t.TempDir()
			if err := os.Chmod(parent, 0o700); err != nil {
				t.Fatal(err)
			}
			result, err := captureInspector(context.Background(), os.Args[0], []string{"-test.run=^TestCaptureInspectorHelperProcess$", "capture-helper", scenario.name}, parent, 32, 1024)
			if (err != nil) != scenario.wantErr {
				t.Fatalf("capture err = %v, wantErr=%t", err, scenario.wantErr)
			}
			if scenario.wantErr {
				if result.Stream != nil {
					t.Fatal("unverified helper exposed stream")
				}
				return
			}
			body, err := io.ReadAll(result.Stream)
			if err != nil || string(body) != "BWEX test bytes" || result.Evidence.ExportBytes != int64(len(body)) {
				t.Fatalf("captured stream = %q, evidence=%+v, err=%v", body, result.Evidence, err)
			}
			if err := result.Stream.Close(); err != nil {
				t.Fatal(err)
			}
			if err := result.Remove(); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(parent + "/stream.bin"); !os.IsNotExist(err) {
				t.Fatalf("verified spool survived exact cleanup: %v", err)
			}
		})
	}
}

func TestParseInspectorEvidenceRequiresExactFields(t *testing.T) {
	exportEvidence := []byte(`BOOT_EVIDENCE {"vm_state":"stopped","runtime_network_devices":0,"export_bytes":3,"console_bytes":0,"inspector_mode":"export"}` + "\n")
	parsed, err := parseInspectorEvidence(exportEvidence, 3)
	if err != nil || parsed.Mode != "export" {
		t.Fatalf("valid export evidence = %+v, %v", parsed, err)
	}
	for _, tc := range []struct{ raw string }{
		{`BOOT_EVIDENCE {"vm_state":"stopped","runtime_network_devices":1,"export_bytes":3,"console_bytes":0}` + "\n"},
		{`BOOT_EVIDENCE {"vm_state":"stopped","runtime_network_devices":0,"export_bytes":2,"console_bytes":0}` + "\n"},
		{`BOOT_EVIDENCE {"vm_state":"stopped","runtime_network_devices":0,"export_bytes":3,"console_bytes":0,"unknown":1}` + "\n"},
		{`BOOT_EVIDENCE {"vm_state":"stopped","runtime_network_devices":0,"export_bytes":3,"console_bytes":0,"inspector_mode":"synthetic"}` + "\n"},
		{`BOOT_EVIDENCE {"vm_state":"stopped","runtime_network_devices":null,"export_bytes":3,"console_bytes":0}` + "\n"},
		{`BOOT_EVIDENCE {"vm_state":"stopped","runtime_network_devices":0,"export_bytes":3,"console_bytes":0}` + "\n" + `BOOT_EVIDENCE {}` + "\n"},
	} {
		if _, err := parseInspectorEvidence([]byte(tc.raw), 3); err == nil {
			t.Fatal(fmt.Sprintf("unsafe inspector evidence accepted: %q", tc.raw))
		}
	}
}
