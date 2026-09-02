package hostx

import (
	"context"
	"strings"
	"testing"

	"github.com/weshofmann/boxwarden/internal/execx"
)

func TestSystemInitValidatesUnprivilegedStateBeforeExactPrivilegeTransition(t *testing.T) {
	inspector, request := healthyDoctorFixture(t)
	runner := &privilegeRunnerFake{result: execx.Result{Stdout: `{"published":true,"already_installed":false,"refresh_login_session":true}`}}
	validated := ""
	service := SystemInitializer{
		inspector: inspector, privilege: runner, executable: "/opt/boxwarden/bin/boxwarden",
		sourceValidator: func(path, digest string) error { validated = path + ":" + digest; return nil },
	}
	result, err := service.Init(context.Background(), request)
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	if !result.HostInstalled || !result.RefreshLoginSession {
		t.Fatalf("Init() = %#v, want host-only publication boundary", result)
	}
	if validated != request.SoftnetPath+":"+SoftnetExecutableSHA256 || runner.command.Path != "/usr/bin/sudo" {
		t.Fatalf("preflight/transition = %q %#v", validated, runner.command)
	}
	if containsString(inspector.commands, commandKey(request.TartPath, "--version")) {
		t.Fatalf("init executed configured Tart: %v", inspector.commands)
	}
}

func TestSystemInitUsesSameExactSafeTartAdmissionAsDoctor(t *testing.T) {
	for name, mutate := range map[string]func(*PathFact){
		"not executable": func(fact *PathFact) { fact.Mode = 0o644 },
		"setuid":         func(fact *PathFact) { fact.Mode = 0o4755 },
		"setgid":         func(fact *PathFact) { fact.Mode = 0o2755 },
		"digest":         func(fact *PathFact) { fact.SHA256 = strings.Repeat("0", 64) },
		"hardlink":       func(fact *PathFact) { fact.Links = 2 },
	} {
		t.Run(name, func(t *testing.T) {
			inspector, request := healthyDoctorFixture(t)
			fact := inspector.paths[request.TartPath]
			mutate(&fact)
			inspector.paths[request.TartPath] = fact
			runner := &privilegeRunnerFake{}
			service := SystemInitializer{inspector: inspector, privilege: runner, executable: "/opt/boxwarden/bin/boxwarden", sourceValidator: func(string, string) error { return nil }}
			if _, err := service.Init(t.Context(), request); err == nil {
				t.Fatal("Init() error = nil, want unsafe Tart refusal")
			}
			if containsString(inspector.commands, commandKey(request.TartPath, "--version")) || runner.command.Path != "" {
				t.Fatalf("unsafe Tart caused execution: doctor=%v privilege=%#v", inspector.commands, runner.command)
			}
		})
	}
}

func TestSystemInitRefusesUnsafeHomebrewOrUnsupportedPlatformWithoutPrivilege(t *testing.T) {
	for name, mutate := range map[string]func(*doctorInspectorFake){
		"unsafe Homebrew": func(inspector *doctorInspectorFake) {
			inspector.homebrew = []HomebrewSoftnet{{Path: "/opt/homebrew/bin/softnet", Privilege: "setuid"}}
		},
		"unsupported platform": func(inspector *doctorInspectorFake) { inspector.platform.OS = "linux" },
	} {
		t.Run(name, func(t *testing.T) {
			inspector, request := healthyDoctorFixture(t)
			mutate(inspector)
			runner := &privilegeRunnerFake{}
			service := SystemInitializer{inspector: inspector, privilege: runner, executable: "/opt/boxwarden/bin/boxwarden", sourceValidator: func(string, string) error { return nil }}
			if _, err := service.Init(context.Background(), request); err == nil {
				t.Fatal("Init() error = nil, want fail-closed refusal")
			}
			if runner.command.Path != "" {
				t.Fatalf("privilege runner called after failed preflight: %#v", runner.command)
			}
		})
	}
}
