package session

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/weshofmann/boxwarden/internal/config"
	"github.com/weshofmann/boxwarden/internal/guestproto"
	"github.com/weshofmann/boxwarden/internal/supervisor"
)

type actionControlFake struct {
	now            time.Time
	snapshots      int
	runs           int
	retries        int
	mutateSnapshot func(int, *supervisor.Snapshot)
	run            func(guestproto.ActionRequest) (guestproto.ActionReceipt, error)
}

func (f *actionControlFake) RetryAction(ctx context.Context, binding supervisor.Binding, request guestproto.ActionRequest) (guestproto.ActionReceipt, error) {
	f.retries++
	return f.RunAction(ctx, binding, request)
}

func (f *actionControlFake) Snapshot(_ context.Context, binding supervisor.Binding) (supervisor.Snapshot, error) {
	f.snapshots++
	snapshot := supervisor.Snapshot{Binding: binding, BackendRunning: true, SerialHealthy: true, PinPresent: true, CertificateCurrent: true, ProbeOK: true, ZoneMatches: true, ObservedAt: f.now}
	if f.mutateSnapshot != nil {
		f.mutateSnapshot(f.snapshots, &snapshot)
	}
	return snapshot, nil
}

func (f *actionControlFake) RunAction(_ context.Context, binding supervisor.Binding, request guestproto.ActionRequest) (guestproto.ActionReceipt, error) {
	f.runs++
	if binding.Generation != request.Generation || binding.SessionID != request.SessionID {
		return guestproto.ActionReceipt{}, errors.New("binding mismatch")
	}
	if f.run != nil {
		return f.run(request)
	}
	_, digest, err := guestproto.EncodeActionRequest(request)
	if err != nil {
		return guestproto.ActionReceipt{}, err
	}
	return guestproto.ActionReceipt{Version: guestproto.Version, Association: request.Association, Generation: request.Generation, RecipeDigest: request.RecipeDigest, ActionID: request.ActionID, ActionPhase: request.ActionPhase, AttemptID: request.AttemptID, RequestSHA256: digest, State: "succeeded"}, nil
}

func testActionService(root string, control *actionControlFake, attemptID string) *ActionService {
	service := NewActionService(config.Domain{ID: "work", StateRoot: root}, control)
	service.now = func() time.Time { return control.now }
	service.newID = func() (string, error) { return attemptID, nil }
	return service
}

func TestExecuteActionReservesBeforeGuestAndRecordsExactReceipt(t *testing.T) {
	root, fixture := actionAttemptFixture(t)
	control := &actionControlFake{now: time.Now()}
	control.run = func(request guestproto.ActionRequest) (guestproto.ActionReceipt, error) {
		stored, err := LoadActionAttempt(root, fixture.Domain, fixture.SessionID, fixture.AttemptID)
		if err != nil || stored.State != ActionAttemptReserved {
			t.Fatalf("guest called before durable reservation: %+v, %v", stored, err)
		}
		if request.RecipeDigest != fixture.RecipeDigest || request.ActionID != fixture.ActionID || request.ActionPhase != fixture.ActionPhase || request.AttemptID != fixture.AttemptID || len(request.Argv) != 1 || request.Argv[0] != "/usr/bin/true" {
			t.Fatalf("request differs from exact recipe: %+v", request)
		}
		_, digest, err := guestproto.EncodeActionRequest(request)
		if err != nil {
			return guestproto.ActionReceipt{}, err
		}
		return guestproto.ActionReceipt{Version: guestproto.Version, Association: request.Association, Generation: request.Generation, RecipeDigest: request.RecipeDigest, ActionID: request.ActionID, ActionPhase: request.ActionPhase, AttemptID: request.AttemptID, RequestSHA256: digest, State: "succeeded"}, nil
	}
	service := testActionService(root, control, fixture.AttemptID)
	got, err := service.ExecuteAction(context.Background(), fixture.SessionName, fixture.ActionPhase, fixture.ActionID)
	if err != nil || got.State != ActionAttemptSucceeded || len(got.ReceiptSHA256) != 64 || control.snapshots != 2 || control.runs != 1 {
		t.Fatalf("ExecuteAction = %+v, %v; snapshots=%d runs=%d", got, err, control.snapshots, control.runs)
	}
	if stored, err := LoadActionAttempt(root, fixture.Domain, fixture.SessionID, fixture.AttemptID); err != nil || stored != got {
		t.Fatalf("stored result = %+v, %v", stored, err)
	}
	if _, err := service.ExecuteAction(context.Background(), fixture.SessionName, fixture.ActionPhase, fixture.ActionID); err == nil || control.runs != 1 {
		t.Fatalf("once replay reached guest: %v", err)
	}
}

func TestExecuteActionEnforcesAutomaticPhaseOrderAtReservation(t *testing.T) {
	root := sessionRoot(t)
	record, value, _ := automaticPlanFixture(t)
	digest, err := PublishRecipeIntent(root, value)
	if err != nil {
		t.Fatal(err)
	}
	record.RecipeIntentDigest = digest
	if err := SaveRecord(root, record.Domain, record); err != nil {
		t.Fatal(err)
	}
	control := &actionControlFake{now: time.Now()}
	service := NewActionService(config.Domain{ID: record.Domain, StateRoot: root}, control)
	service.now = func() time.Time { return control.now }
	ids := []string{
		"00000000-0000-4000-8000-000000000011",
		"00000000-0000-4000-8000-000000000012",
		"00000000-0000-4000-8000-000000000013",
	}
	service.newID = func() (string, error) {
		if len(ids) == 0 {
			t.Fatal("unexpected action attempt allocation")
		}
		id := ids[0]
		ids = ids[1:]
		return id, nil
	}
	if _, err := service.ExecuteAction(t.Context(), "dev", "startup", "startup-a"); err == nil || control.runs != 0 || len(ids) != 3 {
		t.Fatalf("startup bypassed once actions: %v, runs=%d ids=%d", err, control.runs, len(ids))
	}
	if _, err := service.ExecuteAction(t.Context(), "dev", "once", "once-b"); err == nil || control.runs != 0 || len(ids) != 3 {
		t.Fatalf("second once bypassed first: %v, runs=%d ids=%d", err, control.runs, len(ids))
	}
	for _, action := range []struct{ phase, id string }{{"once", "once-a"}, {"once", "once-b"}, {"startup", "startup-a"}} {
		got, err := service.ExecuteAction(t.Context(), "dev", action.phase, action.id)
		if err != nil || got.State != ActionAttemptSucceeded {
			t.Fatalf("ordered %s/%s = %+v, %v", action.phase, action.id, got, err)
		}
	}
	if control.runs != 3 || len(ids) != 0 {
		t.Fatalf("ordered actions reached guest %d times; remaining IDs %d", control.runs, len(ids))
	}
	if _, err := service.ExecuteAction(t.Context(), "dev", "once", "once-a"); err == nil || control.runs != 3 {
		t.Fatalf("completed once action replayed: %v", err)
	}
}

func TestRunAutomaticActionsStopsAtUncertainAttemptAndResumesExactOrder(t *testing.T) {
	root := sessionRoot(t)
	record, value, _ := automaticPlanFixture(t)
	digest, err := PublishRecipeIntent(root, value)
	if err != nil {
		t.Fatal(err)
	}
	record.RecipeIntentDigest = digest
	if err := SaveRecord(root, record.Domain, record); err != nil {
		t.Fatal(err)
	}
	control := &actionControlFake{now: time.Now()}
	service := NewActionService(config.Domain{ID: record.Domain, StateRoot: root}, control)
	service.now = func() time.Time { return control.now }
	ids := []string{"00000000-0000-4000-8000-000000000021", "00000000-0000-4000-8000-000000000022", "00000000-0000-4000-8000-000000000023"}
	service.newID = func() (string, error) {
		id := ids[0]
		ids = ids[1:]
		return id, nil
	}
	control.run = func(request guestproto.ActionRequest) (guestproto.ActionReceipt, error) {
		if request.ActionID == "once-b" {
			return guestproto.ActionReceipt{}, errors.New("guest reply lost")
		}
		_, digest, err := guestproto.EncodeActionRequest(request)
		if err != nil {
			return guestproto.ActionReceipt{}, err
		}
		return guestproto.ActionReceipt{Version: guestproto.Version, Association: request.Association, Generation: request.Generation,
			RecipeDigest: request.RecipeDigest, ActionID: request.ActionID, ActionPhase: request.ActionPhase,
			AttemptID: request.AttemptID, RequestSHA256: digest, State: "succeeded"}, nil
	}
	done, err := service.RunAutomaticActions(t.Context(), record)
	if err == nil || len(done) != 2 || done[0].ActionID != "once-a" || done[0].State != ActionAttemptSucceeded ||
		done[1].ActionID != "once-b" || done[1].State != ActionAttemptIndeterminate || control.runs != 2 || len(ids) != 1 {
		t.Fatalf("uncertain automatic run = %+v, %v; runs=%d ids=%d", done, err, control.runs, len(ids))
	}
	if done, err := service.RunAutomaticActions(t.Context(), record); !errors.Is(err, ErrAutomaticActionUnresolved) || len(done) != 0 || control.runs != 2 {
		t.Fatalf("uncertain automatic action replayed: %+v, %v", done, err)
	}
}

func TestRunAutomaticActionsExecutesOrderedStepsAndNewGenerationStartup(t *testing.T) {
	root := sessionRoot(t)
	record, value, _ := automaticPlanFixture(t)
	digest, err := PublishRecipeIntent(root, value)
	if err != nil {
		t.Fatal(err)
	}
	record.RecipeIntentDigest = digest
	if err := SaveRecord(root, record.Domain, record); err != nil {
		t.Fatal(err)
	}
	control := &actionControlFake{now: time.Now()}
	service := NewActionService(config.Domain{ID: record.Domain, StateRoot: root}, control)
	service.now = func() time.Time { return control.now }
	ids := []string{"00000000-0000-4000-8000-000000000031", "00000000-0000-4000-8000-000000000032", "00000000-0000-4000-8000-000000000033", "00000000-0000-4000-8000-000000000034"}
	service.newID = func() (string, error) {
		if len(ids) == 0 {
			t.Fatal("unexpected action attempt allocation")
		}
		id := ids[0]
		ids = ids[1:]
		return id, nil
	}
	done, err := service.RunAutomaticActions(t.Context(), record)
	if err != nil || len(done) != 3 || done[0].ActionID != "once-a" || done[1].ActionID != "once-b" || done[2].ActionID != "startup-a" || control.runs != 3 {
		t.Fatalf("ordered automatic run = %+v, %v; runs=%d", done, err, control.runs)
	}
	if again, err := service.RunAutomaticActions(t.Context(), record); err != nil || len(again) != 0 || control.runs != 3 {
		t.Fatalf("completed generation replayed: %+v, %v; runs=%d", again, err, control.runs)
	}
	old := record
	record.StartGeneration = "00000000-0000-4000-8000-000000000004"
	if err := SaveRecord(root, record.Domain, record); err != nil {
		t.Fatal(err)
	}
	if _, err := service.RunAutomaticActions(t.Context(), old); err == nil || control.runs != 3 {
		t.Fatalf("stale generation was accepted: %v", err)
	}
	done, err = service.RunAutomaticActions(t.Context(), record)
	if err != nil || len(done) != 1 || done[0].ActionID != "startup-a" || done[0].Generation != record.StartGeneration || control.runs != 4 {
		t.Fatalf("new generation startup = %+v, %v; runs=%d", done, err, control.runs)
	}
}

func TestExecuteActionRejectsStaleReadinessBeforeReservation(t *testing.T) {
	root, fixture := actionAttemptFixture(t)
	control := &actionControlFake{now: time.Now(), mutateSnapshot: func(_ int, s *supervisor.Snapshot) { s.ProbeOK = false }}
	service := testActionService(root, control, fixture.AttemptID)
	if _, err := service.ExecuteAction(context.Background(), fixture.SessionName, fixture.ActionPhase, fixture.ActionID); err == nil || control.runs != 0 {
		t.Fatalf("stale READY reached guest: %v", err)
	}
	if _, err := LoadActionAttempt(root, fixture.Domain, fixture.SessionID, fixture.AttemptID); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("attempt exists without fresh READY: %v", err)
	}
}

func TestExecuteActionKeepsUncertainGuestResultIndeterminate(t *testing.T) {
	root, fixture := actionAttemptFixture(t)
	control := &actionControlFake{now: time.Now(), run: func(guestproto.ActionRequest) (guestproto.ActionReceipt, error) {
		return guestproto.ActionReceipt{}, errors.New("connection dropped")
	}}
	service := testActionService(root, control, fixture.AttemptID)
	got, err := service.ExecuteAction(context.Background(), fixture.SessionName, fixture.ActionPhase, fixture.ActionID)
	if err == nil || !strings.Contains(err.Error(), "connection dropped") || got.State != ActionAttemptIndeterminate {
		t.Fatalf("uncertain action = %+v, %v", got, err)
	}
	if stored, err := LoadActionAttempt(root, fixture.Domain, fixture.SessionID, fixture.AttemptID); err != nil || stored.State != ActionAttemptIndeterminate {
		t.Fatalf("uncertain journal = %+v, %v", stored, err)
	}
	if _, err := service.ExecuteAction(context.Background(), fixture.SessionName, fixture.ActionPhase, fixture.ActionID); err == nil || control.runs != 1 {
		t.Fatalf("uncertain action replayed: %v", err)
	}
}

func TestRetryActionRecoversOnlyExactOriginalAttempt(t *testing.T) {
	root, fixture := actionAttemptFixture(t)
	control := &actionControlFake{now: time.Now(), run: func(guestproto.ActionRequest) (guestproto.ActionReceipt, error) {
		return guestproto.ActionReceipt{}, errors.New("connection dropped")
	}}
	service := testActionService(root, control, fixture.AttemptID)
	uncertain, err := service.ExecuteAction(context.Background(), fixture.SessionName, fixture.ActionPhase, fixture.ActionID)
	if err == nil || uncertain.State != ActionAttemptIndeterminate {
		t.Fatalf("initial uncertain attempt = %+v, %v", uncertain, err)
	}
	if _, err := service.RetryAction(context.Background(), fixture.SessionName, "00000000-0000-4000-8000-000000000005"); err == nil || control.retries != 0 {
		t.Fatalf("foreign attempt reached guest: %v", err)
	}
	control.run = nil
	service.newID = func() (string, error) { t.Fatal("retry allocated new attempt"); return "", nil }
	recovered, err := service.RetryAction(context.Background(), fixture.SessionName, fixture.AttemptID)
	if err != nil || recovered.State != ActionAttemptSucceeded || recovered.AttemptID != fixture.AttemptID || control.retries != 1 {
		t.Fatalf("exact retry = %+v, %v; retries=%d", recovered, err, control.retries)
	}
	if _, err := service.RetryAction(context.Background(), fixture.SessionName, fixture.AttemptID); err == nil || control.retries != 1 {
		t.Fatalf("terminal attempt retried: %v", err)
	}
}

func TestRetryActionAdmitsInterruptedReservationWithoutNewAttempt(t *testing.T) {
	root, fixture := actionAttemptFixture(t)
	if err := ReserveActionAttempt(root, fixture); err != nil {
		t.Fatal(err)
	}
	control := &actionControlFake{now: time.Now()}
	control.run = func(request guestproto.ActionRequest) (guestproto.ActionReceipt, error) {
		stored, err := LoadActionAttempt(root, fixture.Domain, fixture.SessionID, fixture.AttemptID)
		if err != nil || stored.State != ActionAttemptIndeterminate || request.AttemptID != fixture.AttemptID {
			t.Fatalf("retry reached guest without exact indeterminate intent: %+v, %v", stored, err)
		}
		return guestproto.ActionReceipt{}, errors.New("guest claim unresolved")
	}
	service := testActionService(root, control, fixture.AttemptID)
	got, err := service.RetryAction(context.Background(), fixture.SessionName, fixture.AttemptID)
	if err == nil || got.State != ActionAttemptIndeterminate || control.retries != 1 {
		t.Fatalf("interrupted reservation = %+v, %v; retries=%d", got, err, control.retries)
	}
	if stored, err := LoadActionAttempt(root, fixture.Domain, fixture.SessionID, fixture.AttemptID); err != nil || stored != got {
		t.Fatalf("durable interrupted state = %+v, %v", stored, err)
	}
}

func TestRetryActionRejectsChangedGenerationBeforeGuest(t *testing.T) {
	root, fixture := actionAttemptFixture(t)
	if err := ReserveActionAttempt(root, fixture); err != nil {
		t.Fatal(err)
	}
	record, err := LoadRecord(root, string(fixture.Domain), fixture.SessionName)
	if err != nil {
		t.Fatal(err)
	}
	record.StartGeneration = "00000000-0000-4000-8000-000000000004"
	if err := SaveRecord(root, record.Domain, record); err != nil {
		t.Fatal(err)
	}
	control := &actionControlFake{now: time.Now()}
	service := testActionService(root, control, fixture.AttemptID)
	if _, err := service.RetryAction(context.Background(), fixture.SessionName, fixture.AttemptID); err == nil || control.retries != 0 {
		t.Fatalf("changed generation retried: %v", err)
	}
}

func TestSkipActionRequiresStoppedExactSessionAndKeepsReplayBlocked(t *testing.T) {
	root, fixture := actionAttemptFixture(t)
	if err := ReserveActionAttempt(root, fixture); err != nil {
		t.Fatal(err)
	}
	service := testActionService(root, &actionControlFake{now: time.Now()}, fixture.AttemptID)
	if _, err := service.SkipAction(context.Background(), fixture.SessionName, fixture.AttemptID); err == nil {
		t.Fatal("running action skipped while guest may still be executing")
	}
	record, err := LoadRecord(root, string(fixture.Domain), fixture.SessionName)
	if err != nil {
		t.Fatal(err)
	}
	record.IntendedState = StateStopped
	record.StartGeneration = ""
	record.Readiness = ReadinessRecord{Status: ReadinessNotReady}
	if err := SaveRecord(root, record.Domain, record); err != nil {
		t.Fatal(err)
	}
	if _, err := service.SkipAction(context.Background(), fixture.SessionName, "00000000-0000-4000-8000-000000000005"); err == nil {
		t.Fatal("foreign attempt skipped")
	}
	skipped, err := service.SkipAction(context.Background(), fixture.SessionName, fixture.AttemptID)
	if err != nil || skipped.State != ActionAttemptSkipped || skipped.ReceiptSHA256 != "" {
		t.Fatalf("exact skip = %+v, %v", skipped, err)
	}
	if again, err := service.SkipAction(context.Background(), fixture.SessionName, fixture.AttemptID); err != nil || again != skipped {
		t.Fatalf("idempotent skip = %+v, %v", again, err)
	}
	if stored, err := LoadActionAttempt(root, fixture.Domain, fixture.SessionID, fixture.AttemptID); err != nil || stored != skipped {
		t.Fatalf("durable skip = %+v, %v", stored, err)
	}
	record.IntendedState = StateRunning
	record.StartGeneration = fixture.Generation
	record.Readiness = ReadinessRecord{Status: ReadinessReady}
	if err := SaveRecord(root, record.Domain, record); err != nil {
		t.Fatal(err)
	}
	other := fixture
	other.AttemptID = "00000000-0000-4000-8000-000000000005"
	if err := ReserveActionAttempt(root, other); err == nil {
		t.Fatal("skipped once action admitted a new attempt on same system")
	}
}

func TestExecuteActionRejectsReceiptWhenReadinessChangesAfterGuest(t *testing.T) {
	root, fixture := actionAttemptFixture(t)
	control := &actionControlFake{now: time.Now(), mutateSnapshot: func(call int, s *supervisor.Snapshot) {
		if call == 2 {
			s.CertificateCurrent = false
		}
	}}
	service := testActionService(root, control, fixture.AttemptID)
	got, err := service.ExecuteAction(context.Background(), fixture.SessionName, fixture.ActionPhase, fixture.ActionID)
	if err == nil || got.State != ActionAttemptIndeterminate || control.runs != 1 {
		t.Fatalf("changed READY accepted success: %+v, %v", got, err)
	}
}

func TestExecuteActionRejectsForeignGuestReceipt(t *testing.T) {
	root, fixture := actionAttemptFixture(t)
	control := &actionControlFake{now: time.Now(), run: func(request guestproto.ActionRequest) (guestproto.ActionReceipt, error) {
		_, digest, err := guestproto.EncodeActionRequest(request)
		if err != nil {
			return guestproto.ActionReceipt{}, err
		}
		return guestproto.ActionReceipt{Version: guestproto.Version, Association: request.Association, Generation: request.Generation, RecipeDigest: request.RecipeDigest, ActionID: request.ActionID, ActionPhase: request.ActionPhase, AttemptID: request.AttemptID, RequestSHA256: digest, State: "failed"}, nil
	}}
	service := testActionService(root, control, fixture.AttemptID)
	got, err := service.ExecuteAction(context.Background(), fixture.SessionName, fixture.ActionPhase, fixture.ActionID)
	if err == nil || got.State != ActionAttemptIndeterminate || control.runs != 1 {
		t.Fatalf("false receipt accepted: %+v, %v", got, err)
	}
}
