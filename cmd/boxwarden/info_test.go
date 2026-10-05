package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/weshofmann/boxwarden/internal/hostx"
)

func TestInfoWithoutConfiguration(t *testing.T) {
	for _, arg := range []string{"version", "--version", "help", "--help", "-h"} {
		var out bytes.Buffer
		handled, err := runInfo([]string{arg}, &out)
		if !handled || err != nil || !strings.Contains(out.String(), "Boxwarden") {
			t.Fatalf("%s: handled=%v err=%v output=%q", arg, handled, err, out.String())
		}
		if arg == "help" && (!strings.Contains(out.String(), "project") || !strings.Contains(out.String(), "clipboard")) {
			t.Fatal("help omits packaged workflows")
		}
	}
	for _, args := range [][]string{{"project", "create", "demo"}, {"version", "unexpected"}, {"--config", "/fixture/config", "doctor"}} {
		var out bytes.Buffer
		handled, err := runInfo(args, &out)
		if handled || err != nil || out.Len() != 0 {
			t.Fatalf("ordinary argv intercepted: %v", args)
		}
	}
}

func TestBuildInfoReportsCompiledPolicyWithoutConfiguration(t *testing.T) {
	var out bytes.Buffer
	handled, err := runInfo([]string{"build-info", "--json"}, &out)
	if !handled || err != nil {
		t.Fatalf("handled=%v err=%v", handled, err)
	}
	var info struct {
		Version  string `json:"version"`
		Revision string `json:"revision"`
		Network  struct {
			Build       string `json:"build"`
			Description string `json:"description"`
			Version     string `json:"softnet_version"`
			Executable  string `json:"softnet_executable_sha256"`
			Archive     string `json:"softnet_archive_sha256"`
			Target      string `json:"block_target"`
		} `json:"network_policy"`
	}
	if err := json.Unmarshal(out.Bytes(), &info); err != nil {
		t.Fatal(err)
	}
	want := "stock"
	if hostx.SoftnetBlockTarget != "" {
		want = "n1candidate"
	}
	if info.Version != buildVersion || info.Revision != buildRevision || info.Network.Build != want || info.Network.Description != hostx.NetworkPolicyBuild || info.Network.Version != hostx.SoftnetVersion || info.Network.Executable != hostx.SoftnetExecutableSHA256 || info.Network.Archive != hostx.SoftnetArchiveSHA256 || info.Network.Target != hostx.SoftnetBlockTarget {
		t.Fatalf("wrong compiled identity: %s", out.String())
	}
	for _, args := range [][]string{{"build-info"}, {"build-info", "--json", "extra"}, {"build-info", "--other"}} {
		out.Reset()
		handled, err := runInfo(args, &out)
		if handled || err != nil || out.Len() != 0 {
			t.Fatalf("intercepted malformed info args: %v", args)
		}
	}
}
