package app

import (
	"bytes"
	"strings"
	"testing"

	"github.com/weshofmann/boxwarden/internal/backend"
	"github.com/weshofmann/boxwarden/internal/hostx"
	"github.com/weshofmann/boxwarden/internal/lifecycle"
	"github.com/weshofmann/boxwarden/internal/session"
)

func TestStatusDistinguishesPolicyBuildFromLiveReadiness(t *testing.T) {
	var output bytes.Buffer
	err := writeStatus(&output, session.Record{}, backend.Observation{}, lifecycle.Reconciliation{}, session.ReadinessReady)
	if err != nil {
		t.Fatal(err)
	}
	want := "network-policy-build: stock ADR 015; permits gateway service access\n"
	if hostx.SoftnetBlockTarget != "" {
		want = "network-policy-build: N1 candidate; not host-qualified\n"
	}
	if !strings.Contains(output.String(), want) {
		t.Fatalf("status lacks accurate build-scoped limitation: %q", output.String())
	}
}
