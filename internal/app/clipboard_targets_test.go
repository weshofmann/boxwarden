package app

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/weshofmann/boxwarden/internal/backend"
	"github.com/weshofmann/boxwarden/internal/backend/fake"
	"github.com/weshofmann/boxwarden/internal/config"
	"github.com/weshofmann/boxwarden/internal/session"
	"github.com/weshofmann/boxwarden/internal/supervisor"
)

func TestClipboardTargetsListsOnlyFreshExactReadyBindings(t *testing.T) {
	path, selected := writeDomainFixture(t, "work")
	ready := session.Record{Version: 2, Domain: "work", Name: "ready", ID: "11111111-1111-4111-8111-111111111111", Mode: session.ModeClean,
		IntendedState: session.StateRunning, Backend: session.BackendRef{Kind: "tart", ObjectID: "bw-ready"}, GoldenRevision: "golden-r1",
		StartGeneration: "22222222-2222-4222-8222-222222222222", Readiness: session.ReadinessRecord{Status: session.ReadinessReady}}
	stopped := session.Record{Version: 2, Domain: "work", Name: "stopped", ID: "33333333-3333-4333-8333-333333333333", Mode: session.ModeClean,
		IntendedState: session.StateStopped, Backend: session.BackendRef{Kind: "tart", ObjectID: "bw-stopped"}, GoldenRevision: "golden-r1",
		Readiness: session.ReadinessRecord{Status: session.ReadinessNotReady}}
	for _, record := range []session.Record{ready, stopped} {
		if err := session.SaveRecord(selected.StateRoot, selected.ID, record); err != nil {
			t.Fatal(err)
		}
	}
	binding := supervisor.Binding{Domain: "work", SessionID: ready.ID, BackendKind: "tart", BackendObject: ready.Backend.ObjectID, Generation: ready.StartGeneration}
	reader := &statusSnapshotFake{snapshot: supervisor.Snapshot{Binding: binding, BackendRunning: true, SerialHealthy: true, PinPresent: true,
		CertificateCurrent: true, ProbeOK: true, ZoneMatches: true, ObservedAt: time.Now()}}
	var output bytes.Buffer
	options := Options{Output: &output, Observer: fake.Observer{Observations: map[string]backend.Observation{
		"bw-ready":   {ObjectID: "bw-ready", Exists: true, State: backend.ObjectRunning},
		"bw-stopped": {ObjectID: "bw-stopped", Exists: true, State: backend.ObjectStopped}}},
		StatusSnapshotFactory: func(config.Config, config.Domain) (StatusSnapshotReader, error) { return reader, nil }}
	args := []string{"--config", path, "--domain", "work", "clipboard", "targets"}
	check := func(wantReady bool) {
		t.Helper()
		output.Reset()
		if err := Run(t.Context(), args, options); err != nil {
			t.Fatal(err)
		}
		var got struct {
			Targets []struct {
				Domain        string `json:"domain"`
				Session       string `json:"session"`
				SessionID     string `json:"session_id"`
				BackendKind   string `json:"backend_kind"`
				BackendObject string `json:"backend_object"`
				Generation    string `json:"generation"`
				Available     bool   `json:"available"`
			} `json:"targets"`
		}
		if err := json.Unmarshal(output.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		if len(got.Targets) != 2 || got.Targets[0].Session != "ready" || got.Targets[0].Domain != "work" || got.Targets[0].Available != wantReady || got.Targets[1].Session != "stopped" || got.Targets[1].Available {
			t.Fatalf("targets=%+v", got.Targets)
		}
		if wantReady && (got.Targets[0].SessionID != ready.ID || got.Targets[0].Generation != ready.StartGeneration || got.Targets[0].BackendObject != ready.Backend.ObjectID || got.Targets[0].BackendKind != "tart") {
			t.Fatalf("wrong exact target: %+v", got.Targets[0])
		}
	}
	check(true)
	reader.snapshot.ObservedAt = time.Now().Add(-time.Minute)
	check(false)
	if _, err := parseCommand([]string{"--config", path, "clipboard", "targets"}, Options{}); err == nil {
		t.Fatal("target discovery accepted implicit domain")
	}
}
