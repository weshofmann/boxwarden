package sshx

import (
	"context"
	"strings"
	"testing"

	"github.com/weshofmann/boxwarden/internal/guestproto"
)

func testSSHAction(connection Connection) guestproto.ActionRequest {
	return guestproto.ActionRequest{
		Version: guestproto.Version,
		Association: guestproto.Association{
			Domain: string(connection.Binding.Domain), SessionID: connection.Binding.SessionID,
			BackendKind: connection.Binding.BackendKind, BackendObject: connection.Binding.BackendObject,
		},
		Generation:   "00000000-0000-4000-8000-000000000003",
		RecipeDigest: strings.Repeat("a", 64),
		ActionID:     "configure-agent", ActionPhase: "once",
		AttemptID: "00112233-4455-4677-8899-aabbccddeeff",
		Argv:      []string{"/usr/bin/true"},
	}
}

func TestRunActionUsesPinnedFixedHelperAndExactReceipt(t *testing.T) {
	connection := testConnection(t)
	request := testSSHAction(connection)
	wantRequest, digest, err := guestproto.EncodeActionRequest(request)
	if err != nil {
		t.Fatal(err)
	}
	wantReceipt := guestproto.ActionReceipt{
		Version: guestproto.Version, Association: request.Association,
		Generation: request.Generation, RecipeDigest: request.RecipeDigest,
		ActionID: request.ActionID, ActionPhase: request.ActionPhase,
		AttemptID: request.AttemptID, RequestSHA256: digest, State: "succeeded",
	}
	receiptBytes, err := guestproto.EncodeActionReceipt(request, wantReceipt)
	if err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{onRun: func(Command) Result { return Result{Stdout: string(receiptBytes) + "\n"} }}
	got, err := NewClient(runner).RunAction(context.Background(), connection, request)
	if err != nil || got != wantReceipt {
		t.Fatalf("RunAction = %+v, %v", got, err)
	}
	if len(runner.commands) != 1 {
		t.Fatalf("SSH calls = %d", len(runner.commands))
	}
	command := runner.commands[0]
	wantArgs := expectedSSHArgs(connection)
	wantArgs[len(wantArgs)-1] = "action"
	if command.Path != sshPath || !sameStrings(command.Args, wantArgs) || string(command.Stdin) != string(wantRequest) {
		t.Fatalf("action escaped fixed pinned SSH boundary: %+v", command)
	}
}

func TestRunActionRejectsDifferentBindingBeforeSSH(t *testing.T) {
	connection := testConnection(t)
	request := testSSHAction(connection)
	request.BackendObject = "another-vm"
	runner := &fakeRunner{}
	if _, err := NewClient(runner).RunAction(context.Background(), connection, request); err == nil || len(runner.commands) != 0 {
		t.Fatalf("different association reached SSH: %v, %+v", err, runner.commands)
	}
}

func TestRunActionRejectsKnownHostsDriftBeforeSSH(t *testing.T) {
	connection := testConnection(t)
	request := testSSHAction(connection)
	mustWrite(t, connection.KnownHostsFile, []byte(HostKeyAlias(connection.Binding.SessionID)+" "+changedPublicKey+"\n"), 0o600)
	runner := &fakeRunner{}
	if _, err := NewClient(runner).RunAction(context.Background(), connection, request); err == nil || len(runner.commands) != 0 {
		t.Fatalf("changed host key reached action SSH: %v, %+v", err, runner.commands)
	}
}

func TestRunActionRejectsChangedOrOversizedReceipt(t *testing.T) {
	connection := testConnection(t)
	request := testSSHAction(connection)
	_, digest, err := guestproto.EncodeActionRequest(request)
	if err != nil {
		t.Fatal(err)
	}
	receipt := guestproto.ActionReceipt{
		Version: guestproto.Version, Association: request.Association,
		Generation: request.Generation, RecipeDigest: request.RecipeDigest,
		ActionID: request.ActionID, ActionPhase: request.ActionPhase,
		AttemptID: request.AttemptID, RequestSHA256: digest, State: "succeeded",
	}
	encoded, err := guestproto.EncodeActionReceipt(request, receipt)
	if err != nil {
		t.Fatal(err)
	}
	for name, result := range map[string]Result{
		"changed digest":   {Stdout: strings.Replace(string(encoded), digest, strings.Repeat("b", 64), 1)},
		"oversized":        {Stdout: strings.Repeat("x", guestproto.MaxActionReceiptBytes+1)},
		"truncated":        {Stdout: string(encoded), Truncated: true},
		"stderr oversized": {Stdout: string(encoded), Stderr: strings.Repeat("x", guestproto.MaxActionReceiptBytes+1)},
	} {
		t.Run(name, func(t *testing.T) {
			runner := &fakeRunner{onRun: func(Command) Result { return result }}
			if _, err := NewClient(runner).RunAction(context.Background(), connection, request); err == nil {
				t.Fatal("accepted unsafe action SSH result")
			}
		})
	}
}
