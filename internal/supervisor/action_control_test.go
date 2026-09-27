package supervisor

import (
	"context"
	"encoding/json"
	"net"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weshofmann/boxwarden/internal/guestproto"
)

type actionRuntimeFixture struct {
	runtimeFixture
	actions    atomic.Int32
	retries    atomic.Int32
	badReceipt atomic.Bool
	loseReady  atomic.Bool
}

func (o *actionRuntimeFixture) RetryAction(ctx context.Context, request guestproto.ActionRequest) (guestproto.ActionReceipt, error) {
	o.retries.Add(1)
	return o.RunAction(ctx, request)
}

func (o *actionRuntimeFixture) RunAction(_ context.Context, request guestproto.ActionRequest) (guestproto.ActionReceipt, error) {
	o.actions.Add(1)
	if o.loseReady.Load() && o.pinPresent != nil {
		o.pinPresent.Store(false)
	}
	_, digest, err := guestproto.EncodeActionRequest(request)
	if err != nil {
		return guestproto.ActionReceipt{}, err
	}
	receipt := guestproto.ActionReceipt{Version: guestproto.Version, Association: request.Association,
		Generation: request.Generation, RecipeDigest: request.RecipeDigest, ActionID: request.ActionID,
		ActionPhase: request.ActionPhase, AttemptID: request.AttemptID, RequestSHA256: digest, State: "succeeded"}
	if o.badReceipt.Load() {
		receipt.RequestSHA256 = strings.Repeat("b", 64)
	}
	return receipt, nil
}

func actionControlRequest(t *testing.T) (LaunchRequest, guestproto.ActionRequest) {
	t.Helper()
	launch := minimalRequest(t)
	root := filepath.Dir(filepath.Dir(filepath.Dir(launch.RuntimeDirectory)))
	launch.Binding.SessionID = "13b0bf73-3bd5-4f1c-8bdc-71d50c36d6d0"
	launch.Binding.Generation = "00112233-4455-4677-8899-aabbccddeeff"
	launch.RuntimeDirectory = filepath.Join(root, launch.Binding.Domain, launch.Binding.SessionID, launch.Binding.Generation)
	action := guestproto.ActionRequest{Version: guestproto.Version,
		Association: guestproto.Association{Domain: launch.Binding.Domain, SessionID: launch.Binding.SessionID,
			BackendKind: launch.Binding.BackendKind, BackendObject: launch.Binding.BackendObject},
		Generation: launch.Binding.Generation, RecipeDigest: strings.Repeat("a", 64),
		ActionID: "configure-agent", ActionPhase: "once", AttemptID: "00000000-0000-4000-8000-000000000005",
		Argv: []string{"/usr/bin/true", strings.Repeat("x", 20<<10)},
	}
	return launch, action
}

func TestActionControlRequiresExactReadyGenerationAndBoundedReceipt(t *testing.T) {
	launch, action := actionControlRequest(t)
	path, _, err := publishOrAdmitRequest(launch)
	if err != nil {
		t.Fatal(err)
	}
	owner := &actionRuntimeFixture{runtimeFixture: runtimeFixture{done: make(chan struct{}), pinPresent: new(atomic.Bool)}}
	owner.pinPresent.Store(true)
	runDone := make(chan error, 1)
	go func() { runDone <- Run(context.Background(), path, owner) }()
	client := &Client{RuntimeDirectory: launch.RuntimeDirectory, MaxSnapshotAge: time.Minute}
	if _, err := awaitSnapshot(context.Background(), launch.Binding, startupPolicy{timeout: time.Second, interval: time.Millisecond}, client.Snapshot); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Stop(context.Background(), launch.Binding); <-runDone })
	wrong := launch.Binding
	wrong.Generation = "00000000-0000-4000-8000-000000000006"
	if _, err := client.RunAction(context.Background(), wrong, action); err == nil || owner.actions.Load() != 0 {
		t.Fatalf("foreign generation reached owner: %v", err)
	}
	bad := action
	bad.Argv = []string{"relative-command"}
	if _, err := client.RunAction(context.Background(), launch.Binding, bad); err == nil || owner.actions.Load() != 0 {
		t.Fatalf("invalid action reached owner: %v", err)
	}
	root := filepath.Dir(filepath.Dir(filepath.Dir(launch.RuntimeDirectory)))
	exact, err := NewExactActionController(root)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := exact.RunAction(context.Background(), launch.Binding, action)
	if err != nil || receipt.State != "succeeded" || owner.actions.Load() != 1 {
		t.Fatalf("exact large action = %+v, %v; calls=%d", receipt, err, owner.actions.Load())
	}
	owner.badReceipt.Store(true)
	if _, err := client.RunAction(context.Background(), launch.Binding, action); err == nil || owner.actions.Load() != 2 {
		t.Fatalf("mismatched receipt accepted: %v", err)
	}
	owner.badReceipt.Store(false)
	owner.loseReady.Store(true)
	if _, err := client.RunAction(context.Background(), launch.Binding, action); err == nil || owner.actions.Load() != 3 {
		t.Fatalf("lost READY after action accepted: %v", err)
	}
	if _, err := client.RunAction(context.Background(), launch.Binding, action); err == nil || owner.actions.Load() != 3 {
		t.Fatalf("lost READY before action reached owner: %v", err)
	}
}

func TestActionControlExplicitRetryUsesRetrierOnly(t *testing.T) {
	launch, action := actionControlRequest(t)
	path, _, err := publishOrAdmitRequest(launch)
	if err != nil {
		t.Fatal(err)
	}
	owner := &actionRuntimeFixture{runtimeFixture: runtimeFixture{done: make(chan struct{}), pinPresent: new(atomic.Bool)}}
	owner.pinPresent.Store(true)
	runDone := make(chan error, 1)
	go func() { runDone <- Run(context.Background(), path, owner) }()
	client := &Client{RuntimeDirectory: launch.RuntimeDirectory, MaxSnapshotAge: time.Minute}
	if _, err := awaitSnapshot(context.Background(), launch.Binding, startupPolicy{timeout: time.Second, interval: time.Millisecond}, client.Snapshot); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Stop(context.Background(), launch.Binding); <-runDone })
	if _, err := client.RetryAction(context.Background(), launch.Binding, action); err != nil || owner.retries.Load() != 1 || owner.actions.Load() != 1 {
		t.Fatalf("exact retry route = %v; retries=%d actions=%d", err, owner.retries.Load(), owner.actions.Load())
	}
}

func TestExactActionControllerRejectsNonCanonicalRoot(t *testing.T) {
	if _, err := NewExactActionController("relative/runtime"); err == nil {
		t.Fatal("relative runtime root admitted")
	}
	if _, err := NewExactActionController("/"); err == nil {
		t.Fatal("root filesystem admitted")
	}
	if controller, err := NewExactActionController("/private/boxwarden-runtime"); err != nil || controller == nil {
		t.Fatalf("canonical controller = %v, %v", controller, err)
	}
}

// The real control handler must keep the failed precheck reason even when
// its final observation has recovered. No guest action may run in between.
type recoveringActionRuntime struct {
	actionRuntimeFixture
	fail func(*Snapshot)
}

func (o *recoveringActionRuntime) Snapshot(ctx context.Context) Snapshot {
	snapshot := o.runtimeFixture.Snapshot(ctx)
	if o.snapshots.Load() == 2 {
		o.fail(&snapshot)
	}
	return snapshot
}
func TestActionControlRetainsFailedPrecheckAfterReadyRecovery(t *testing.T) {
	for _, actionName := range []string{"run_action", "retry_action"} {
		for _, tc := range []struct {
			name, want string
			fail       func(*Snapshot)
		}{
			{"probe", "ssh probe; strict management SSH probe failed", func(s *Snapshot) { s.ProbeOK = false; s.Diagnostic = "strict management SSH probe failed" }},
			{"unknown", "unproven checks: serial", func(s *Snapshot) { s.SerialHealthy = false; s.Diagnostic = "secret-fixture-token\x1b[31m" }},
			{"binding", "exact-generation binding mismatch", func(s *Snapshot) { s.Binding.Generation = "foreign-secret-fixture-token" }},
		} {
			t.Run(actionName+"/"+tc.name, func(t *testing.T) {
				launch, action := actionControlRequest(t)
				owner := &recoveringActionRuntime{actionRuntimeFixture: actionRuntimeFixture{runtimeFixture: runtimeFixture{binding: launch.Binding}}, fail: tc.fail}
				if !snapshotReady(owner.Snapshot(t.Context())) {
					t.Fatal("first snapshot must be READY")
				}
				server, client := net.Pipe()
				defer client.Close()
				served := make(chan struct{})
				go func() {
					handleControl(t.Context(), server, launch.Binding, owner, func() error { return nil })
					close(served)
				}()
				client.SetDeadline(time.Now().Add(time.Second))
				request, err := json.Marshal(controlRequest{Version: 1, Action: actionName, Binding: launch.Binding, ExpiresAt: time.Now().Add(time.Second), GuestAction: &action})
				if err != nil {
					t.Fatal(err)
				}
				if err = writeFrame(client, request); err != nil {
					t.Fatal(err)
				}
				data, err := readBounded(client)
				if err != nil {
					t.Fatal(err)
				}
				<-served
				var response controlResponse
				if err = decodeExact(data, &response); err != nil {
					t.Fatal(err)
				}
				if owner.actions.Load() != 0 || owner.retries.Load() != 0 || response.GuestReceipt != nil || !snapshotReady(response.Snapshot) || owner.snapshots.Load() != 3 {
					t.Fatalf("failed precheck executed action or lost final READY: %+v", response)
				}
				if !strings.Contains(response.Error, tc.want) || strings.Contains(response.Error, "secret-fixture-token") {
					t.Fatalf("failed precheck reason lost or unsafe: %q", response.Error)
				}
			})
		}
	}
}
