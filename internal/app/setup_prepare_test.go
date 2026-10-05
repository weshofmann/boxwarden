package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"testing"
)

// Missing the prepare route would prevent native onboarding from driving the
// admitted package helper without a terminal or parsing its human output.
func TestPackagedPreparationDoesNotRequestBuildToolPaths(t *testing.T) {
	var out bytes.Buffer
	called := false
	err := Run(t.Context(), []string{"--config", "/new/config.json", "setup", "prepare", "--json", "--package", "/package", "--iso", "/ubuntu.iso", "--prebuilt"}, Options{Output: &out, SetupPrepare: func(_ context.Context, path string, input SetupPrepareInput, _ io.Writer) (SetupInspection, bool, error) {
		called = path == "/new/config.json" && input.Prebuilt && input.GoBinary == "" && input.ISOPath == "/ubuntu.iso"
		return SetupInspection{Version: 1, Scope: "alpha_project_setup", Status: "ready"}, true, nil
	}})
	if err != nil || !called {
		t.Fatalf("packaged preparation = %v, called=%v", err, called)
	}
}

func TestSetupPrepareUsesStructuredEvents(t *testing.T) {
	var out bytes.Buffer
	args := []string{"--config", "/config.json", "setup", "prepare", "--json", "--package", "/package", "--iso", "/ubuntu.iso", "--checker", "/checker.deb", "--go", "/tool/go", "--zstd", "/tool/zstd", "--openssl", "/tool/openssl", "--xorriso", "/tool/xorriso"}
	err := Run(context.Background(), args, Options{Output: &out})
	var event map[string]any
	if decodeErr := json.Unmarshal(out.Bytes(), &event); decodeErr != nil {
		t.Fatalf("no structured prepare error: %v, run %v", decodeErr, err)
	}
	if err == nil || event["operation"] != "setup.prepare" || event["type"] != "error" {
		t.Fatalf("event %v, err %v", event, err)
	}
}

func TestSetupPrepareStreamsProgressAndAuthoritativeResult(t *testing.T) {
	var out bytes.Buffer
	called := false
	o := Options{Output: &out, SetupPrepare: func(_ context.Context, path string, input SetupPrepareInput, progress io.Writer) (SetupInspection, bool, error) {
		called = true
		if path != "/config.json" || input.PackageRoot != "/package" || input.OpenSSLPath != "/tool/openssl" {
			t.Fatalf("input %s %+v", path, input)
		}
		if _, err := io.WriteString(progress, "human output with no machine meaning\n"); err != nil {
			return SetupInspection{}, true, err
		}
		return SetupInspection{Version: 1, Scope: "alpha_project_setup", Status: "ready", ConfigValid: true, SelectionAcceptable: true, NextActions: []string{}}, true, nil
	}}
	args := []string{"--config", "/config.json", "setup", "prepare", "--json", "--package", "/package", "--iso", "/ubuntu.iso", "--checker", "/checker.deb", "--go", "/tool/go", "--zstd", "/tool/zstd", "--openssl", "/tool/openssl", "--xorriso", "/tool/xorriso"}
	if err := Run(t.Context(), args, o); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("preparation callback not invoked")
	}
	decoder := json.NewDecoder(&out)
	var events []map[string]any
	for decoder.More() {
		var event map[string]any
		if err := decoder.Decode(&event); err != nil {
			t.Fatal(err)
		}
		events = append(events, event)
	}
	if len(events) != 2 || events[0]["type"] != "progress" || events[1]["type"] != "result" {
		t.Fatalf("events %v", events)
	}
	if events[1]["data"].(map[string]any)["status"] != "ready" {
		t.Fatalf("result %v", events[1])
	}
}

func TestSetupPrepareRetainsSideEffectUncertaintyOnFailure(t *testing.T) {
	var out bytes.Buffer
	o := Options{Output: &out, SetupPrepare: func(context.Context, string, SetupPrepareInput, io.Writer) (SetupInspection, bool, error) {
		return SetupInspection{}, true, errors.New("synthetic helper failed after state publication")
	}}
	args := []string{"--config", "/config.json", "setup", "prepare", "--json", "--package", "/package", "--iso", "/ubuntu.iso", "--checker", "/checker.deb", "--go", "/tool/go", "--zstd", "/tool/zstd", "--openssl", "/tool/openssl", "--xorriso", "/tool/xorriso"}
	if err := Run(t.Context(), args, o); err == nil {
		t.Fatal("helper failure hidden")
	}
	var event map[string]any
	if err := json.Unmarshal(out.Bytes(), &event); err != nil {
		t.Fatal(err)
	}
	if event["type"] != "error" || event["data"].(map[string]any)["uncertain"] != true {
		t.Fatalf("event %v", event)
	}
}
