package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/weshofmann/boxwarden/internal/backend"
	"github.com/weshofmann/boxwarden/internal/config"
	"github.com/weshofmann/boxwarden/internal/lifecycle"
	"github.com/weshofmann/boxwarden/internal/session"
)

type clipboardTargetEntry struct {
	Domain        string `json:"domain"`
	Session       string `json:"session"`
	SessionID     string `json:"session_id"`
	BackendKind   string `json:"backend_kind"`
	BackendObject string `json:"backend_object"`
	Generation    string `json:"generation"`
	Available     bool   `json:"available"`
}

// Discovery reports metadata for one explicitly selected domain. Current
// readiness is a UI hint; the transfer service re-admits the exact binding.
func writeClipboardTargets(ctx context.Context, output io.Writer, loaded config.Config, selected config.Domain, observer backend.Observer, snapshots StatusSnapshotFactory) error {
	if observer == nil {
		return fmt.Errorf("backend observer is required")
	}
	records, err := session.ListRecords(selected.StateRoot, selected.ID)
	if err != nil {
		return fmt.Errorf("list exact domain sessions: %w", err)
	}
	entries := make([]clipboardTargetEntry, 0, len(records))
	for _, record := range records {
		entry := clipboardTargetEntry{Domain: string(selected.ID), Session: string(record.Name), SessionID: record.ID,
			BackendKind: record.Backend.Kind, BackendObject: record.Backend.ObjectID, Generation: record.StartGeneration}
		if record.Backend.Kind == "tart" && record.IntendedState == session.StateRunning && record.Readiness.Status == session.ReadinessReady &&
			session.RequireNoRebuild(selected.StateRoot, selected.ID, string(record.Name)) == nil {
			observed, observeErr := observer.Observe(ctx, record.Backend.ObjectID)
			if observeErr == nil && observed.Exists && observed.ObjectID == record.Backend.ObjectID {
				reconciled := lifecycle.Reconcile(record.IntendedState, observed)
				reconciled, readiness := reconcileStatusSnapshot(ctx, loaded, selected, record, observed, reconciled, snapshots)
				entry.Available = readiness == session.ReadinessReady && reconciled.Consistency == lifecycle.Consistent
			}
		}
		entries = append(entries, entry)
	}
	return json.NewEncoder(output).Encode(struct {
		Targets []clipboardTargetEntry `json:"targets"`
	}{Targets: entries})
}
