package architecture

import (
	"strings"
	"testing"
)

func TestDiagnosticNetworkExactResolveExemption(t *testing.T) {
	source := "//go:build (n1diagnostic || n1clipboarddiagnostic) && !n1candidate\npackage sessionruntime; func f(){resolver.Resolve(nil,\"object\")}"
	issues := strings.Join(inspectSliceBSource("internal/sessionruntime/owner_network_diagnostic.go", []byte(source)), "\n")
	if issues != "" {
		t.Fatalf("exact tagged read-only Resolve refused: %s", issues)
	}
}
