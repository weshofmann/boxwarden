package main

import (
	"context"
	"errors"
	"github.com/weshofmann/boxwarden/internal/app"
	"github.com/weshofmann/boxwarden/internal/execx"
	"os"
	"path/filepath"
	"testing"
)

type setupResourceRunnerFunc func(context.Context, execx.Command) (execx.Result, error)

func (f setupResourceRunnerFunc) Run(ctx context.Context, c execx.Command) (execx.Result, error) {
	return f(ctx, c)
}

func TestMissingCommandLineToolsStopsBeforePackageGitOrHelpers(t *testing.T) {
	var calls []string
	runner := setupResourceRunnerFunc(func(_ context.Context, c execx.Command) (execx.Result, error) {
		calls = append(calls, c.Path)
		return execx.Result{}, errors.New("synthetic missing CLT")
	})
	_, _, err := preflightFirstRunResourcesWithRunner(t.Context(), app.FirstRunInput{PackageRoot: "/missing-package", ISOPath: "/missing.iso"}, runner)
	if err == nil || len(calls) != 1 || calls[0] != "/usr/bin/xcode-select" {
		t.Fatalf("preflight=%v calls=%v", err, calls)
	}
}

func TestResourcePreflightRejectsMissingPackageBeforeWriting(t *testing.T) {
	_, _, err := preflightFirstRunResources(context.Background(), app.FirstRunInput{PackageRoot: "/missing-package", ISOPath: "/missing.iso"})
	if err == nil {
		t.Fatal("missing package admitted")
	}
}

func TestDiscoveredToolMustBeExactExecutable(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "openssl")
	if err := os.WriteFile(path, []byte("synthetic"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := discoverSetupTool("openssl", []string{path}); err == nil {
		t.Fatal("nonexecutable admitted")
	}
	if err := os.Chmod(path, 0700); err != nil {
		t.Fatal(err)
	}
	got, digest, err := discoverSetupTool("openssl", []string{path})
	if err != nil || got != path || len(digest) != 64 {
		t.Fatalf("discovery=%q,%q,%v", got, digest, err)
	}
	if _, _, err := discoverSetupTool("xorriso", []string{path}); err == nil {
		t.Fatal("wrong tool name admitted")
	}
}
