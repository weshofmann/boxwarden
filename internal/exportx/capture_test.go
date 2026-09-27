package exportx

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestCaptureInspectorHelperProcess(t *testing.T) {
	if len(os.Args) < 4 || os.Args[2] != "capture-helper" {
		return
	}
	if os.Args[3] == "pipe-holder" {
		time.Sleep(time.Second)
		os.Exit(0)
	}
	if strings.HasPrefix(os.Args[3], "cancel-") {
		term := make(chan os.Signal, 1)
		signal.Notify(term, syscall.SIGTERM)
		_, _ = io.WriteString(os.Stdout, "partial")
		_ = os.WriteFile("helper-ready", []byte("ready"), 0o600)
		<-term
		switch os.Args[3] {
		case "cancel-valid", "cancel-held-pipe":
			if os.Args[3] == "cancel-held-pipe" {
				child := exec.Command(os.Args[0], "-test.run=^TestCaptureInspectorHelperProcess$", "capture-helper", "pipe-holder")
				child.Stdout, child.Stderr = os.Stdout, os.Stderr
				if err := child.Start(); err != nil {
					os.Exit(9)
				}
			}
			_, _ = io.WriteString(os.Stderr, "BOOT_CANCELLED {\"console_bytes\":0,\"export_bytes\":7,\"runtime_network_devices\":0,\"vm_state\":\"stopped\",\"inspector_mode\":\"export\"}\n")
		case "cancel-overflow":
			_, _ = io.WriteString(os.Stderr, strings.Repeat("x", 2048))
		case "cancel-malformed":
			_, _ = io.WriteString(os.Stderr, "BOOT_CANCELLED {}\n")
		case "cancel-ignore":
			for {
				time.Sleep(time.Second)
			}
		}
		os.Exit(1)
	}
	switch os.Args[3] {
	case "ok":
		_, _ = io.WriteString(os.Stdout, "BWEX test bytes")
		_, _ = io.WriteString(os.Stderr, "BOOT_EVIDENCE {\"console_bytes\":10,\"export_bytes\":15,\"runtime_network_devices\":0,\"vm_state\":\"stopped\"}\n")
	case "export":
		_, _ = io.WriteString(os.Stdout, "BWEX\x00\x010123456789abcdef")
		_, _ = io.WriteString(os.Stderr, "BOOT_EVIDENCE {\"console_bytes\":10,\"export_bytes\":22,\"runtime_network_devices\":0,\"vm_state\":\"stopped\",\"inspector_mode\":\"export\"}\n")
	case "empty-export":
		_, _ = io.WriteString(os.Stderr, "BOOT_EVIDENCE {\"console_bytes\":10,\"export_bytes\":0,\"runtime_network_devices\":0,\"vm_state\":\"stopped\",\"inspector_mode\":\"export\"}\n")
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
	}{{"ok", false}, {"export", true}, {"empty-export", false}} {
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

func TestCaptureCancellationRequiresStoppedReceipt(t *testing.T) {
	for _, mode := range []string{"cancel-valid", "cancel-malformed", "cancel-missing", "cancel-ignore", "cancel-overflow", "cancel-held-pipe"} {
		t.Run(mode, func(t *testing.T) {
			parent := t.TempDir()
			if err := os.Chmod(parent, 0o700); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			readyDone := make(chan struct{})
			go func() {
				defer close(readyDone)
				deadline := time.Now().Add(5 * time.Second)
				for time.Now().Before(deadline) {
					if _, err := os.Stat(parent + "/helper-ready"); err == nil {
						cancel()
						return
					}
					time.Sleep(time.Millisecond)
				}
				cancel()
			}()
			started := time.Now()
			result, err := captureInspectorWithCleanupDeadline(ctx, os.Args[0], []string{"-test.run=^TestCaptureInspectorHelperProcess$", "capture-helper", mode}, parent, 32, 1024, 150*time.Millisecond)
			if time.Since(started) > 3*time.Second {
				t.Fatal("cleanup was not bounded")
			}
			<-readyDone
			if _, readyErr := os.Stat(parent + "/helper-ready"); readyErr != nil {
				t.Fatalf("helper never installed signal handler: %v", readyErr)
			}
			if result.Stream != nil || err == nil {
				t.Fatalf("cancellation exposed stream: %+v, %v", result, err)
			}
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("missing cancellation cause: %v", err)
			}
			_, statErr := os.Lstat(parent + "/stream.bin")
			if mode == "cancel-valid" {
				if !os.IsNotExist(statErr) {
					t.Fatalf("verified cancelled spool survived: %v", statErr)
				}
			} else if !errors.Is(err, ErrInspectorStopUnproven) {
				t.Fatalf("missing unproven stop classification: %v", err)
			} else if statErr != nil {
				t.Fatalf("unproven cancellation evidence erased: %v", statErr)
			}
		})
	}
}

func TestCancellationReceiptCannotQualifyPublication(t *testing.T) {
	valid := `BOOT_CANCELLED {"vm_state":"stopped","runtime_network_devices":0,"export_bytes":7,"console_bytes":0,"inspector_mode":"export"}` + "\n"
	if _, err := parseInspectorEvidence([]byte(valid), 7); err == nil {
		t.Fatal("cancellation receipt admitted for publication")
	}
	for _, raw := range []string{
		strings.Replace(valid, `"stopped"`, `"running"`, 1),
		strings.Replace(valid, `"runtime_network_devices":0`, `"runtime_network_devices":1`, 1),
		strings.Replace(valid, `"export_bytes":7`, `"export_bytes":8`, 1),
		strings.Replace(valid, `"console_bytes":0`, `"console_bytes":262145`, 1),
		strings.Replace(valid, `"export"`, `"synthetic"`, 1),
		strings.Replace(valid, `"export_bytes":7`, `"export_bytes":7,"export_bytes":7`, 1),
		valid + valid,
		valid + strings.Replace(valid, "BOOT_CANCELLED", "BOOT_EVIDENCE", 1),
	} {
		if _, err := parseInspectorCancellation([]byte(raw), 7); err == nil {
			t.Fatalf("unsafe cancellation receipt admitted: %s", raw)
		}
	}
	evidence, err := parseInspectorCancellation([]byte(valid), 7)
	if err != nil || evidence.Mode != "export" {
		t.Fatalf("valid stopped cancellation receipt rejected: %+v %v", evidence, err)
	}
}
