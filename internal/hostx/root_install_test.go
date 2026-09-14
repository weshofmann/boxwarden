package hostx

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
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

func TestInvokeRootInstallPreservesOnlyRecognizedBoundedRootDiagnostic(t *testing.T) {
	request := InstallRequest{Version: 1, SoftnetSource: "/source/softnet", Tart: qualifiedTartForTest(), TartHome: "/Users/wes/tart"}
	trusted := "boxwarden: ensure operator group: new local operator group did not become visible within bounded exact reads: local operator group record not visible\n"
	runner := &privilegeRunnerFake{result: execx.Result{Stderr: trusted}, err: errors.New("exit status 1")}
	_, err := InvokeRootInstall(context.Background(), runner, "/opt/boxwarden/bin/boxwarden", request)
	if err == nil || !strings.Contains(err.Error(), "ensure operator group: new local operator group did not become visible") || !strings.Contains(err.Error(), "exit status 1") {
		t.Fatalf("InvokeRootInstall() error = %v; want trusted stage and process exit status", err)
	}

	for name, stderr := range map[string]string{
		"sudo message":      "sudo: a password is required\n",
		"forged suffix":     "sudo: denied\n" + trusted,
		"credential-shaped": "boxwarden: ensure operator group: password=hunter2\n",
		"overlong":          "boxwarden: ensure operator group: " + strings.Repeat("x", 4096) + "\n",
		"embedded newline":  strings.TrimSuffix(trusted, "\n") + "\nspoofed: success\n",
		"carriage return":   strings.TrimSuffix(trusted, "\n") + "\rspoofed: success\n",
		"escape sequence":   strings.TrimSuffix(trusted, "\n") + "\x1b[2K\n",
	} {
		t.Run(name, func(t *testing.T) {
			runner := &privilegeRunnerFake{result: execx.Result{Stderr: stderr}, err: errors.New("exit status 1")}
			_, err := InvokeRootInstall(context.Background(), runner, "/opt/boxwarden/bin/boxwarden", request)
			if err == nil || strings.Contains(err.Error(), "hunter2") || strings.Contains(err.Error(), "spoofed") || strings.Contains(err.Error(), "sudo: denied") || strings.Contains(err.Error(), "a password is required") || strings.Contains(err.Error(), "\x1b") || len(err.Error()) > 256 {
				t.Fatalf("InvokeRootInstall(%s) error = %q; want bounded generic failure without arbitrary stderr", name, err)
			}
		})
	}
}

func TestInvokeRootInstallPreservesDirectoryCommandStageAndExitStatus(t *testing.T) {
	request := InstallRequest{Version: 1, SoftnetSource: "/source/softnet", Tart: qualifiedTartForTest(), TartHome: "/Users/wes/tart"}
	for _, stage := range []string{"create dedicated operator group", "add exact trusted operator"} {
		t.Run(stage, func(t *testing.T) {
			stderr := "boxwarden: ensure operator group: " + stage + ": directory-service command failed: run \"/usr/sbin/dseditgroup\": exit status 70\n"
			runner := &privilegeRunnerFake{result: execx.Result{Stderr: stderr}, err: errors.New("exit status 1")}
			_, err := InvokeRootInstall(context.Background(), runner, "/opt/boxwarden/bin/boxwarden", request)
			if err == nil || !strings.Contains(err.Error(), stage) || !strings.Contains(err.Error(), "exit status 70") {
				t.Fatalf("InvokeRootInstall() error = %v; want fixed directory-service stage and child exit status", err)
			}
		})
	}
}

func TestInvokeRootInstallFailsClosedOnTruncatedFailureDiagnostic(t *testing.T) {
	request := InstallRequest{Version: 1, SoftnetSource: "/source/softnet", Tart: qualifiedTartForTest(), TartHome: "/Users/wes/tart"}
	runner := &privilegeRunnerFake{result: execx.Result{Stderr: "boxwarden: ensure operator group: partial", Truncated: true}, err: errors.New("exit status 1")}
	_, err := InvokeRootInstall(context.Background(), runner, "/opt/boxwarden/bin/boxwarden", request)
	if err == nil || !strings.Contains(err.Error(), "truncated") || strings.Contains(err.Error(), "partial") {
		t.Fatalf("InvokeRootInstall() error = %v; want explicit fail-closed truncation", err)
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

func TestRootInstallerDoesNotMutateGroupForUnsafeSoftnetSource(t *testing.T) {
	for name, makeUnsafe := range map[string]func(*testing.T, string, *RootedPublisher){
		"digest mismatch": func(t *testing.T, source string, _ *RootedPublisher) {
			t.Helper()
			if err := os.WriteFile(source, []byte("unqualified replacement"), 0o755); err != nil {
				t.Fatal(err)
			}
		},
		"hard link": func(t *testing.T, source string, _ *RootedPublisher) {
			t.Helper()
			if err := os.Link(source, source+"-second-link"); err != nil {
				t.Fatal(err)
			}
		},
		"non-regular": func(t *testing.T, source string, _ *RootedPublisher) {
			t.Helper()
			if err := os.Remove(source); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(source, 0o755); err != nil {
				t.Fatal(err)
			}
		},
		"privilege bit": func(t *testing.T, _ string, publisher *RootedPublisher) {
			t.Helper()
			publisher.openSource = func(_ string, _ string, rejectPrivilege bool) (*os.File, error) {
				if !rejectPrivilege {
					t.Fatal("preflight source reopen did not request privilege-bit rejection")
				}
				return nil, os.ErrPermission
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			root, source, digest := publisherFixture(t)
			publisher := testPublisher(root, digest)
			makeUnsafe(t, source, &publisher)
			groups := &groupManagerFake{}
			caller := Caller{UID: os.Getuid(), Name: "operator", Home: filepath.Join(filepath.Dir(source), "home")}
			installer := RootInstaller{
				Identity:  identityFake{euid: 0, caller: caller},
				Groups:    groups,
				Publisher: publisher,
			}

			if _, err := installer.Install(t.Context(), publisherRequest(source)); err == nil {
				t.Fatal("Install() error = nil, want unsafe source refusal")
			}
			if groups.ensureCalls != 0 {
				t.Fatalf("group mutations = %d, want zero before unsafe source refusal", groups.ensureCalls)
			}
		})
	}
}

func TestRootInstallerDoesNotMutateGroupForUnsafeDestinationAncestorMetadata(t *testing.T) {
	for name, makeUnsafe := range map[string]func(*RootedPublisher, string){
		"wrong group": func(publisher *RootedPublisher, _ string) { publisher.rootGID++ },
		"extended ACL": func(publisher *RootedPublisher, ancestor string) {
			publisher.acl = pathACLInspector{ancestor: true}
		},
	} {
		t.Run(name, func(t *testing.T) {
			root, source, digest := publisherFixture(t)
			ancestor := filepath.Join(root, "toolchains")
			if err := os.MkdirAll(ancestor, 0o755); err != nil {
				t.Fatal(err)
			}
			publisher := testPublisher(root, digest)
			makeUnsafe(&publisher, ancestor)
			groups := &groupManagerFake{}
			caller := Caller{UID: os.Getuid(), Name: "operator", Home: filepath.Join(filepath.Dir(source), "home")}
			installer := RootInstaller{
				Identity: identityFake{euid: 0, caller: caller}, Groups: groups,
				Publisher: publisher,
			}

			if _, err := installer.Install(t.Context(), publisherRequest(source)); err == nil {
				t.Fatal("Install() error = nil, want unsafe destination ancestor refusal")
			}
			if groups.ensureCalls != 0 {
				t.Fatalf("group mutations = %d, want zero before unsafe ancestor refusal", groups.ensureCalls)
			}
		})
	}
}

func TestRootInstallerDoesNotMutateGroupForUnsafeExistingDestinationAncestor(t *testing.T) {
	for _, relative := range []string{".", "toolchains", filepath.Join("toolchains", "softnet"), filepath.Join("toolchains", "softnet", SoftnetVersion)} {
		t.Run(relative, func(t *testing.T) {
			root, source, digest := publisherFixture(t)
			ancestor := filepath.Join(root, relative)
			if err := os.MkdirAll(ancestor, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(ancestor, 0o775); err != nil {
				t.Fatal(err)
			}
			groups := &groupManagerFake{}
			caller := Caller{UID: os.Getuid(), Name: "operator", Home: filepath.Join(filepath.Dir(source), "home")}
			installer := RootInstaller{
				Identity: identityFake{euid: 0, caller: caller}, Groups: groups,
				Publisher: testPublisher(root, digest),
			}

			if _, err := installer.Install(t.Context(), publisherRequest(source)); err == nil {
				t.Fatal("Install() error = nil, want unsafe destination ancestor refusal")
			}
			if groups.ensureCalls != 0 {
				t.Fatalf("group mutations = %d, want zero before unsafe ancestor refusal", groups.ensureCalls)
			}
		})
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
