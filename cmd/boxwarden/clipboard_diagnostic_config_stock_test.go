//go:build n1clipboarddiagnostic && !n1diagnostic && !n1candidate

package main

import "testing"

func TestDiagnosticCompilePinnedStockConfig(t *testing.T) {
	if clipboardDiagnosticConfigPath != "/Users/devel/Backup/boxwarden_archive/n1-diagnostic-20260930/package/config/stock.enrolled.json" {
		t.Fatal("compile-pinned config role changed")
	}
}
