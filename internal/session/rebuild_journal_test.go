package session

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/weshofmann/boxwarden/internal/domain"
	"github.com/weshofmann/boxwarden/internal/sshx"
	"github.com/weshofmann/boxwarden/internal/supervisor"
)

func testRebuildJournal() RebuildJournal {
	return RebuildJournal{
		Version: 1, Domain: domain.ID("work"), SessionName: "dev",
		SessionID:   "13b0bf73-3bd5-4f1c-8bdc-71d50c36d6d0",
		OperationID: "00112233-4455-4677-8899-aabbccddeeff",
		Phase:       RebuildReserved,
		OldBackend:  "boxwarden-work-dev", OldRevision: "golden-r1",
		CandidateBackend: "boxwarden-work-00112233445546778899aabbccddeeff", CandidateRevision: "golden-r2",
	}
}

func TestRebuildJournalRejectsCandidateReservedByAnotherSession(t *testing.T) {
	root := sessionRoot(t)
	j := testRebuildJournal()
	writeRecord(t, root, "other", `{"version":2,"domain":"work","name":"other","id":"00000000-0000-4000-8000-000000000002","mode":"clean","intended_state":"stopped","backend":{"kind":"tart","object_id":"`+j.CandidateBackend+`"},"golden_revision":"golden-r1","readiness":{"status":"not_ready","diagnostic":""}}`)
	if err := createRebuildJournal(root, j); err == nil {
		t.Fatal("rebuild reserved another session's backend")
	}
	if _, err := LoadRebuildJournal(root, j.Domain, j.SessionName); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed reservation left journal: %v", err)
	}
}

func TestRebuildJournalOldPinWitnessBindsFullRecordAndBackend(t *testing.T) {
	j := testRebuildJournal()
	pin := sshx.HostKeyPin{Version: 1, Domain: j.Domain, SessionID: j.SessionID, BackendKind: "tart", BackendObject: j.OldBackend,
		Algorithm: "ssh-ed25519", PublicKey: "example-public-key", Fingerprint: "example-fingerprint"}
	raw, err := json.Marshal(pin)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(raw)
	j.OldPinPresent, j.OldPinDigest = true, hex.EncodeToString(digest[:])
	if err := j.verifyOldPinWitness(pin); err != nil {
		t.Fatal(err)
	}
	pin.BackendObject = j.CandidateBackend
	if err := j.verifyOldPinWitness(pin); err == nil {
		t.Fatal("same key under candidate backend satisfied old pin witness")
	}
}

func TestRebuildJournalReservationIsStrictDurableAndBlocksOrdinaryMutation(t *testing.T) {
	root := sessionRoot(t)
	j := testRebuildJournal()
	if err := createRebuildJournal(root, j); err != nil {
		t.Fatal(err)
	}
	if err := createRebuildJournal(root, j); err == nil {
		t.Fatal("duplicate rebuild journal replaced first reservation")
	}
	loaded, err := LoadRebuildJournal(root, j.Domain, j.SessionName)
	if err != nil || loaded != j {
		t.Fatalf("durable rebuild journal = %#v, %v", loaded, err)
	}
	if err := RequireNoRebuild(root, j.Domain, j.SessionName); err == nil {
		t.Fatal("ordinary mutation accepted pending rebuild")
	}
	if err := RequireNoRebuild(root, j.Domain, "other"); err != nil {
		t.Fatalf("unrelated session blocked: %v", err)
	}
	path := filepath.Join(root, "rebuilds", j.SessionName+".json")
	if info, err := os.Lstat(path); err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		t.Fatalf("journal permissions = %#v, %v", info, err)
	}
}

func TestRebuildJournalRejectsCorruptAndForeignState(t *testing.T) {
	root := sessionRoot(t)
	j := testRebuildJournal()
	if err := createRebuildJournal(root, j); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(RebuildJournal) RebuildJournal{
		"same backend":           func(j RebuildJournal) RebuildJournal { j.CandidateBackend = j.OldBackend; return j },
		"wrong candidate ID":     func(j RebuildJournal) RebuildJournal { j.CandidateBackend = "boxwarden-work-other"; return j },
		"invalid phase":          func(j RebuildJournal) RebuildJournal { j.Phase = "unknown"; return j },
		"pin digest without pin": func(j RebuildJournal) RebuildJournal { j.OldPinDigest = "abcdef"; return j },
	} {
		t.Run(name, func(t *testing.T) {
			bad := mutate(j)
			if err := validateRebuildJournal(bad); err == nil {
				t.Fatalf("invalid rebuild journal accepted: %#v", bad)
			}
		})
	}
	if _, err := LoadRebuildJournal(root, domain.ID("personal"), j.SessionName); err == nil {
		t.Fatal("foreign domain loaded rebuild journal")
	}
	path := filepath.Join(root, "rebuilds", j.SessionName+".json")
	raw, err := json.Marshal(j)
	if err != nil {
		t.Fatal(err)
	}
	raw[len(raw)-1] = ','
	raw = append(raw, []byte(`"phase":"reserved"}`)...)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadRebuildJournal(root, j.Domain, j.SessionName); err == nil {
		t.Fatal("duplicate journal field accepted")
	}
	if err := RequireNoRebuild(root, j.Domain, j.SessionName); err == nil || errors.Is(err, os.ErrNotExist) {
		t.Fatalf("corrupt journal did not fail closed: %v", err)
	}
}

func TestRebuildJournalGateDoesNotMutateSession(t *testing.T) {
	root := sessionRoot(t)
	writeRecord(t, root, "dev", `{"version":2,"domain":"work","name":"dev","id":"13b0bf73-3bd5-4f1c-8bdc-71d50c36d6d0","mode":"clean","intended_state":"stopped","backend":{"kind":"tart","object_id":"boxwarden-work-dev"},"golden_revision":"golden-r1","readiness":{"status":"not_ready","diagnostic":""}}`)
	if err := createRebuildJournal(root, testRebuildJournal()); err != nil {
		t.Fatal(err)
	}
	before, err := LoadRecord(root, "work", "dev")
	if err != nil {
		t.Fatal(err)
	}
	if err := RequireNoRebuild(root, domain.ID("work"), "dev"); err == nil {
		t.Fatal("pending rebuild was not gated")
	}
	after, err := LoadRecord(root, "work", "dev")
	if err != nil || after != before {
		t.Fatalf("read-only rebuild gate mutated session: %#v, %v", after, err)
	}
}

func TestRebuildJournalReservesOldAndCandidateBackendAcrossSessions(t *testing.T) {
	root := sessionRoot(t)
	j := testRebuildJournal()
	if err := createRebuildJournal(root, j); err != nil {
		t.Fatal(err)
	}
	for _, backendID := range []string{j.OldBackend, j.CandidateBackend} {
		if err := requireUnreservedBackendObject(root, j.Domain, "other", backendID); err == nil {
			t.Fatalf("rebuild backend %q was available to another session", backendID)
		}
	}
}

func TestPendingRebuildBlocksOrdinaryCreateAndStart(t *testing.T) {
	domainConfig, backendFake, creator := createFixture(t)
	created, err := creator.Create(context.Background(), "dev", ModeClean)
	if err != nil {
		t.Fatal(err)
	}
	j := testRebuildJournal()
	j.SessionID, j.OldBackend, j.OldRevision = created.ID, created.Backend.ObjectID, created.GoldenRevision
	j.OperationID = "7fb25db7-3cc1-4d92-a04c-b60fd05fa421"
	j.CandidateBackend = objectIDFor(j.Domain, j.OperationID)
	if err := createRebuildJournal(domainConfig.StateRoot, j); err != nil {
		t.Fatal(err)
	}
	if _, err := creator.Create(context.Background(), "dev", ModeClean); err == nil {
		t.Fatal("create retried through pending rebuild")
	}
	control := &startSupervisorFake{start: func(supervisor.LaunchRequest) (supervisor.Snapshot, error) {
		t.Fatal("pending rebuild reached supervisor launch")
		return supervisor.Snapshot{}, nil
	}}
	starter := newStartTestService(domainConfig, backendFake, control, time.Now, func() (string, error) {
		t.Fatal("pending rebuild allocated a generation")
		return "", nil
	})
	if _, err := starter.Start(context.Background(), "dev"); err == nil {
		t.Fatal("ordinary start accepted pending rebuild")
	}
	if control.startCalls != 0 || len(backendFake.CloneCalls()) != 1 {
		t.Fatalf("blocked operations mutated backend: start=%d clone=%d", control.startCalls, len(backendFake.CloneCalls()))
	}
	current, err := LoadRecord(domainConfig.StateRoot, "work", "dev")
	if err != nil || current != created {
		t.Fatalf("blocked operations mutated session: %#v, %v", current, err)
	}
}
