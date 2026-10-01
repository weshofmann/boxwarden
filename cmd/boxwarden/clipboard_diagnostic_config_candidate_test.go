//go:build n1clipboarddiagnostic && n1diagnostic && !n1candidate

package main

import "testing"

func TestDiagnosticCompilePinnedCandidateConfig(t *testing.T) {
	if clipboardDiagnosticConfigPath != "/Users/devel/Backup/boxwarden_archive/n1-diagnostic-20260930/package/config/candidate.enrolled.json" {
		t.Fatal("compile-pinned config role changed")
	}
}
