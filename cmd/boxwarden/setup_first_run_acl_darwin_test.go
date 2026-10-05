//go:build darwin

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestFirstRunPlanAcceptsProtectedSyntheticAncestors(t *testing.T) {
	input, d := firstRunFixture(t)
	// All ACL targets are created by this test below its own new TempDir.
	// The injected directory replaces the production home dependency entirely.
	syntheticRoot, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	syntheticUser := filepath.Join(syntheticRoot, "synthetic-user")
	d.home = func() (string, error) { return syntheticUser, nil }
	paths := []string{syntheticUser, filepath.Join(syntheticUser, "Library"), filepath.Join(syntheticUser, "Library", "Application Support")}
	for _, fixturePath := range paths {
		if err := os.MkdirAll(fixturePath, 0700); err != nil {
			t.Fatal(err)
		}
		if out, err := exec.Command("/bin/chmod", "+a", "everyone deny delete", fixturePath).CombinedOutput(); err != nil {
			t.Fatalf("fixture ACL: %v %s", err, out)
		}
		p := fixturePath
		t.Cleanup(func() {
			if out, err := exec.Command("/bin/chmod", "-N", p).CombinedOutput(); err != nil {
				t.Errorf("fixture cleanup: %v %s", err, out)
			}
		})
	}
	plan, _, _, err := buildFirstRunPlan(t.Context(), input, d)
	if err != nil || plan.Status != "ready" {
		t.Fatalf("protected synthetic plan: %#v,%v", plan, err)
	}
	input.ExpectedDigest = plan.ExpectedDigest
	result, _, err := createFirstRunWithDependencies(t.Context(), input, os.Stdout, d)
	if err != nil || result.Status != "ready" {
		t.Fatalf("synthetic creation: %#v,%v", result, err)
	}
}
