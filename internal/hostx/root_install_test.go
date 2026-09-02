package hostx

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/weshofmann/boxwarden/internal/execx"
)

func TestRootInstallerPublishesDigestTreeAndIsExactlyIdempotent(t *testing.T) {
	fs := newMemoryPublisher()
	groups := &groupManagerFake{}
	installer := RootInstaller{Identity: identityFake{euid: 0, caller: Caller{UID: 501, Name: "wes", Home: "/Users/wes"}}, Groups: groups, Publisher: fs}
	request := InstallRequest{Version: 1, SoftnetSource: "/source/softnet", Tart: qualifiedTartForTest(), TartHome: "/Users/wes/tart"}

	if result, err := installer.Install(context.Background(), request); err != nil || !result.Published || !result.RefreshLoginSession {
		t.Fatalf("Install() = %#v, %v; want published refresh result", result, err)
	}
	if groups.ensureCalls != 1 || fs.publishCalls != 1 {
		t.Fatalf("side effects = groups %d publish %d, want 1 each", groups.ensureCalls, fs.publishCalls)
	}
	if result, err := installer.Install(context.Background(), request); err != nil || result.Published || !result.AlreadyInstalled {
		t.Fatalf("second Install() = %#v, %v; want exact idempotence", result, err)
	}
}

func TestReadRootInstallRequestEnforcesBoundBeforeDecode(t *testing.T) {
	if _, err := ReadRootInstallRequest(bytes.NewReader(make([]byte, maxInstallRequestBytes+1))); err == nil {
		t.Fatal("ReadRootInstallRequest(oversized) error = nil")
	}
}

func TestInvokeRootInstallUsesExactSudoCommandAndParsesBoundedResult(t *testing.T) {
	runner := &privilegeRunnerFake{result: execx.Result{Stdout: `{"published":true,"already_installed":false,"refresh_login_session":true}`}}
	request := InstallRequest{Version: 1, SoftnetSource: "/source/softnet", Tart: qualifiedTartForTest(), TartHome: "/Users/wes/tart"}
	result, err := InvokeRootInstall(context.Background(), runner, "/opt/boxwarden/bin/boxwarden", request)
	if err != nil {
		t.Fatalf("InvokeRootInstall() error = %v", err)
	}
	if !result.Published || !result.RefreshLoginSession {
		t.Fatalf("InvokeRootInstall() = %#v", result)
	}
	command := runner.command
	if command.Path != "/usr/bin/sudo" || strings.Join(command.Args, " ") != "-- /opt/boxwarden/bin/boxwarden internal host-install" || command.Env == nil || len(command.Env) != 0 {
		t.Fatalf("privilege command = %#v, want exact sudo argv and closed environment", command)
	}
	decoded, err := DecodeInstallRequest(command.Stdin)
	if err != nil || decoded != request {
		t.Fatalf("stdin request = %#v, %v; want exact request", decoded, err)
	}
}

func TestDecodeInstallRequestRejectsDuplicateAndTrailingJSON(t *testing.T) {
	request := InstallRequest{Version: 1, SoftnetSource: "/source/softnet", Tart: qualifiedTartForTest(), TartHome: "/Users/wes/tart"}
	data, err := EncodeInstallRequest(request)
	if err != nil {
		t.Fatal(err)
	}
	for name, input := range map[string][]byte{
		"duplicate":    []byte(strings.Replace(string(data), `"version":1`, `"version":1,"version":1`, 1)),
		"trailing":     append(append([]byte(nil), data...), []byte(`{}`)...),
		"control path": []byte(strings.Replace(string(data), `/source/softnet`, `/source/softnet\nspoof`, 1)),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := DecodeInstallRequest(input); err == nil {
				t.Fatal("DecodeInstallRequest() error = nil, want strict rejection")
			}
		})
	}
}

func TestRootInstallerRefusesNonRootCallerOrUnexpectedExistingState(t *testing.T) {
	request := InstallRequest{Version: 1, SoftnetSource: "/source/softnet", Tart: qualifiedTartForTest(), TartHome: "/Users/wes/tart"}
	for name, installer := range map[string]RootInstaller{
		"non-root":  {Identity: identityFake{euid: 501}, Groups: &groupManagerFake{}, Publisher: newMemoryPublisher()},
		"ambiguous": {Identity: identityFake{euid: 0, caller: Caller{UID: 501, Name: "wes", Home: "/Users/wes"}}, Groups: &groupManagerFake{}, Publisher: &memoryPublisher{state: publicationUnexpected}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := installer.Install(context.Background(), request); err == nil {
				t.Fatal("Install() error = nil, want refusal")
			}
		})
	}
}

func TestRootInstallerDoesNotMutateGroupForPartialExistingTree(t *testing.T) {
	groups := &groupManagerFake{}
	installer := RootInstaller{
		Identity: identityFake{euid: 0, caller: Caller{UID: 501, Name: "wes", Home: "/Users/wes"}},
		Groups:   groups, Publisher: &memoryPublisher{preflight: publicationUnexpected},
	}
	request := InstallRequest{Version: 1, SoftnetSource: "/source/softnet", Tart: qualifiedTartForTest(), TartHome: "/Users/wes/tart"}
	if _, err := installer.Install(context.Background(), request); err == nil {
		t.Fatal("Install() error = nil, want partial-tree refusal")
	}
	if groups.ensureCalls != 0 {
		t.Fatalf("group mutations = %d, want zero before partial-tree refusal", groups.ensureCalls)
	}
}

func qualifiedTartForTest() ToolIdentity {
	return ToolIdentity{Path: "/opt/qualified/tart", Version: TartVersion, ExecutableSHA256: TartExecutableSHA256, ArchiveSHA256: TartArchiveSHA256}
}

type identityFake struct {
	euid   int
	caller Caller
	err    error
}

func (f identityFake) EffectiveUID() int           { return f.euid }
func (f identityFake) SudoCaller() (Caller, error) { return f.caller, f.err }

type groupManagerFake struct{ ensureCalls int }

func (f *groupManagerFake) Ensure(Caller, string) (Group, bool, error) {
	f.ensureCalls++
	return Group{ID: 20, Name: OperatorGroupName, Members: []int{501}}, true, nil
}

type memoryPublisher struct {
	preflight    publicationState
	state        publicationState
	publishCalls int
}

func (f *memoryPublisher) Preflight(context.Context, InstallRequest, Caller) (publicationState, error) {
	return f.preflight, nil
}

type privilegeRunnerFake struct {
	command execx.Command
	result  execx.Result
	err     error
}

func (f *privilegeRunnerFake) Run(_ context.Context, command execx.Command) (execx.Result, error) {
	f.command = command
	return f.result, f.err
}

func newMemoryPublisher() *memoryPublisher { return &memoryPublisher{} }
func (f *memoryPublisher) State(context.Context, InstallRequest, Caller, Group) (publicationState, error) {
	return f.state, nil
}
func (f *memoryPublisher) Publish(context.Context, InstallRequest, Caller, Group) error {
	f.publishCalls++
	f.state = publicationComplete
	return nil
}
