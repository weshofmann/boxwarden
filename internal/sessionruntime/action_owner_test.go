package sessionruntime

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/weshofmann/boxwarden/internal/guestproto"
	"github.com/weshofmann/boxwarden/internal/recipe"
	"github.com/weshofmann/boxwarden/internal/session"
	"github.com/weshofmann/boxwarden/internal/sshx"
)

type recordingActionClient struct {
	*readyClient
	calls int
	run   func(guestproto.ActionRequest) (guestproto.ActionReceipt, error)
}

func (c *recordingActionClient) RunAction(_ context.Context, _ sshx.Connection, request guestproto.ActionRequest) (guestproto.ActionReceipt, error) {
	c.calls++
	return c.run(request)
}

func TestOwnerActionAdmissionRechecksExactReservedRecipeStep(t *testing.T) {
	f := newFixture(t)
	intent := recipe.Recipe{
		Version: 1,
		Source:  recipe.Source{Kind: "ubuntu-24.04.4-desktop-arm64", SHA256: "c2610520bf582976839a1724c669e1cfed0547427be5a0ad12d457b92b46ffbe"},
		Machine: recipe.Machine{CPUs: 4, MemoryMiB: 4096, SystemDiskGiB: 30},
		Steps:   []recipe.Step{{ID: "configure-agent", Phase: "once", Argv: []string{"/usr/bin/true"}}},
	}
	digest, err := session.PublishRecipeIntent(f.root, intent)
	if err != nil {
		t.Fatal(err)
	}
	f.record.RecipeIntentDigest = digest
	f.record.IntendedState = session.StateRunning
	f.record.Readiness = session.ReadinessRecord{Status: session.ReadinessReady}
	if err := session.SaveRecord(f.root, f.record.Domain, f.record); err != nil {
		t.Fatal(err)
	}
	attemptID := "00112233-4455-4677-8899-aabbccddeeee"
	request := guestproto.ActionRequest{Version: guestproto.Version,
		Association: guestproto.Association{Domain: "work", SessionID: f.record.ID, BackendKind: "tart", BackendObject: f.record.Backend.ObjectID},
		Generation:  f.record.StartGeneration, RecipeDigest: digest, ActionID: "configure-agent", ActionPhase: "once", AttemptID: attemptID, Argv: []string{"/usr/bin/true"}}
	if err := admitOwnerAction(f.root, "dev", f.request.Binding, request); err == nil {
		t.Fatal("unreserved action admitted")
	}
	attempt := session.ActionAttempt{Version: 1, Domain: f.record.Domain, SessionName: "dev", SessionID: f.record.ID,
		BackendObject: f.record.Backend.ObjectID, Generation: f.record.StartGeneration, RecipeDigest: digest,
		ActionID: request.ActionID, ActionPhase: request.ActionPhase, AttemptID: attemptID, State: session.ActionAttemptReserved}
	if err := session.ReserveActionAttempt(f.root, attempt); err != nil {
		t.Fatal(err)
	}
	if err := admitOwnerAction(f.root, "dev", f.request.Binding, request); err != nil {
		t.Fatalf("exact reservation refused: %v", err)
	}
	for name, mutate := range map[string]func(*guestproto.ActionRequest){
		"changed argv":       func(r *guestproto.ActionRequest) { r.Argv = []string{"/usr/bin/false"} },
		"changed recipe":     func(r *guestproto.ActionRequest) { r.RecipeDigest = strings.Repeat("b", 64) },
		"changed generation": func(r *guestproto.ActionRequest) { r.Generation = "00000000-0000-4000-8000-000000000004" },
		"changed attempt":    func(r *guestproto.ActionRequest) { r.AttemptID = "00000000-0000-4000-8000-000000000005" },
	} {
		t.Run(name, func(t *testing.T) {
			changed := request
			mutate(&changed)
			if err := admitOwnerAction(f.root, "dev", f.request.Binding, changed); err == nil {
				t.Fatal("foreign action admitted")
			}
		})
	}
	attempt.State = session.ActionAttemptIndeterminate
	writePrivateImportFixture(t, f.root, filepath.Join("action-attempts", f.record.ID), attemptID+".json", attempt)
	if err := admitOwnerAction(f.root, "dev", f.request.Binding, request); err == nil {
		t.Fatal("terminal attempt admitted for guest execution")
	}
	if err := admitOwnerActionState(f.root, "dev", f.request.Binding, request, session.ActionAttemptIndeterminate); err != nil {
		t.Fatalf("exact explicit retry refused: %v", err)
	}
	changed := request
	changed.Argv = []string{"/usr/bin/false"}
	if err := admitOwnerActionState(f.root, "dev", f.request.Binding, changed, session.ActionAttemptIndeterminate); err == nil {
		t.Fatal("changed argv admitted for explicit retry")
	}
}

func TestOwnerActionRunsOnlyReservedStepWhileExactRuntimeStaysReady(t *testing.T) {
	f, client := readyFixture(t)
	for _, operation := range []func(context.Context) error{
		func(ctx context.Context) error { return f.owner.Start(ctx, f.request) },
		f.owner.Bootstrap,
		f.owner.Ready,
	} {
		if err := operation(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { _ = f.owner.Stop(context.Background()); _ = f.owner.Wait(context.Background()) })
	intent := recipe.Recipe{
		Version: 1,
		Source:  recipe.Source{Kind: "ubuntu-24.04.4-desktop-arm64", SHA256: "c2610520bf582976839a1724c669e1cfed0547427be5a0ad12d457b92b46ffbe"},
		Machine: recipe.Machine{CPUs: 4, MemoryMiB: 4096, SystemDiskGiB: 30},
		Steps:   []recipe.Step{{ID: "configure-agent", Phase: "once", Argv: []string{"/usr/bin/true"}}},
	}
	digest, err := session.PublishRecipeIntent(f.root, intent)
	if err != nil {
		t.Fatal(err)
	}
	f.record.RecipeIntentDigest = digest
	f.record.IntendedState = session.StateRunning
	f.record.Readiness = session.ReadinessRecord{Status: session.ReadinessReady}
	if err := session.SaveRecord(f.root, f.record.Domain, f.record); err != nil {
		t.Fatal(err)
	}
	request := guestproto.ActionRequest{Version: guestproto.Version,
		Association: guestproto.Association{Domain: "work", SessionID: f.record.ID, BackendKind: "tart", BackendObject: f.record.Backend.ObjectID},
		Generation:  f.record.StartGeneration, RecipeDigest: digest, ActionID: "configure-agent", ActionPhase: "once",
		AttemptID: "00112233-4455-4677-8899-aabbccddeeee", Argv: []string{"/usr/bin/true"}}
	actionClient := &recordingActionClient{readyClient: client}
	actionClient.run = func(request guestproto.ActionRequest) (guestproto.ActionReceipt, error) {
		if err := admitOwnerAction(f.root, "dev", f.request.Binding, request); err != nil {
			t.Fatalf("owner failed to re-admit before SSH: %v", err)
		}
		_, digest, err := guestproto.EncodeActionRequest(request)
		if err != nil {
			return guestproto.ActionReceipt{}, err
		}
		return guestproto.ActionReceipt{Version: guestproto.Version, Association: request.Association, Generation: request.Generation,
			RecipeDigest: request.RecipeDigest, ActionID: request.ActionID, ActionPhase: request.ActionPhase,
			AttemptID: request.AttemptID, RequestSHA256: digest, State: "succeeded"}, nil
	}
	f.owner.deps.client = actionClient
	if _, err := f.owner.RunAction(context.Background(), request); err == nil || actionClient.calls != 0 {
		t.Fatalf("unreserved request reached SSH: %v", err)
	}
	attempt := session.ActionAttempt{Version: 1, Domain: f.record.Domain, SessionName: "dev", SessionID: f.record.ID,
		BackendObject: f.record.Backend.ObjectID, Generation: f.record.StartGeneration, RecipeDigest: digest,
		ActionID: request.ActionID, ActionPhase: request.ActionPhase, AttemptID: request.AttemptID, State: session.ActionAttemptReserved}
	if err := session.ReserveActionAttempt(f.root, attempt); err != nil {
		t.Fatal(err)
	}
	if receipt, err := f.owner.RunAction(context.Background(), request); err != nil || receipt.State != "succeeded" || actionClient.calls != 1 {
		t.Fatalf("exact owner action = %+v, %v; calls=%d", receipt, err, actionClient.calls)
	}
	priorRun := actionClient.run
	actionClient.run = func(request guestproto.ActionRequest) (guestproto.ActionReceipt, error) {
		receipt, err := priorRun(request)
		client.probe = func(sshx.Connection) error { return errors.New("lost readiness") }
		return receipt, err
	}
	if _, err := f.owner.RunAction(context.Background(), request); err == nil || actionClient.calls != 2 {
		t.Fatalf("lost READY after SSH returned a receipt: %v", err)
	}
	if _, err := f.owner.RunAction(context.Background(), request); err == nil || actionClient.calls != 2 {
		t.Fatalf("lost READY reached SSH: %v", err)
	}
	client.probe = nil
	attempt.State = session.ActionAttemptIndeterminate
	writePrivateImportFixture(t, f.root, filepath.Join("action-attempts", f.record.ID), request.AttemptID+".json", attempt)
	actionClient.run = func(request guestproto.ActionRequest) (guestproto.ActionReceipt, error) {
		if err := admitOwnerActionState(f.root, "dev", f.request.Binding, request, session.ActionAttemptIndeterminate); err != nil {
			t.Fatalf("retry reached SSH without exact indeterminate admission: %v", err)
		}
		_, digest, err := guestproto.EncodeActionRequest(request)
		if err != nil {
			return guestproto.ActionReceipt{}, err
		}
		return guestproto.ActionReceipt{Version: guestproto.Version, Association: request.Association, Generation: request.Generation,
			RecipeDigest: request.RecipeDigest, ActionID: request.ActionID, ActionPhase: request.ActionPhase,
			AttemptID: request.AttemptID, RequestSHA256: digest, State: "succeeded"}, nil
	}
	if _, err := f.owner.RunAction(context.Background(), request); err == nil || actionClient.calls != 2 {
		t.Fatalf("ordinary action admitted indeterminate attempt: %v", err)
	}
	if receipt, err := f.owner.RetryAction(context.Background(), request); err != nil || receipt.State != "succeeded" || actionClient.calls != 3 {
		t.Fatalf("exact owner retry = %+v, %v; calls=%d", receipt, err, actionClient.calls)
	}
}
